package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/cart"
	"github.com/SV1Stail/online_market/internal/catalog"
	"github.com/SV1Stail/online_market/internal/money"
)

type memoryState struct {
	mutex       sync.Mutex
	products    map[string]catalog.Product
	categories  map[string]catalog.Category
	carts       map[string]cartRecord
	orders      map[string]storedOrder
	idempotency map[string]string
	events      map[string][]orderEventRecord
}

type cartRecord struct {
	cart      cart.Cart
	tokenHash []byte
}

func newMemoryState(t requireTestingT, now time.Time) *memoryState {
	price, err := money.New(59_900, "RUB")
	if err != nil {
		t.Fatalf("create seed price: %v", err)
	}
	categoryID := "10000000-0000-4000-8000-000000000001"
	productID := "20000000-0000-4000-8000-000000000001"
	return &memoryState{
		products: map[string]catalog.Product{
			productID: {
				ID: productID, Name: "Seed product", Description: "Seed product for e2e",
				Price: price, Availability: catalog.AvailabilityAvailable,
				CategoryIDs: []string{categoryID}, Version: 1, CreatedAt: now, UpdatedAt: now,
			},
		},
		categories: map[string]catalog.Category{
			categoryID: {
				ID: categoryID, Name: "Seed category", SortOrder: 10,
				Version: 1, CreatedAt: now, UpdatedAt: now,
			},
		},
		carts:       make(map[string]cartRecord),
		orders:      make(map[string]storedOrder),
		idempotency: make(map[string]string),
		events:      make(map[string][]orderEventRecord),
	}
}

type requireTestingT interface {
	Fatalf(string, ...any)
}

type memoryCatalogRepository struct {
	state *memoryState
	clock fixedClock
}

var _ catalog.Repository = (*memoryCatalogRepository)(nil)
var _ catalog.AdminRepository = (*memoryCatalogRepository)(nil)

func (r *memoryCatalogRepository) ListProducts(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	result := make([]catalog.Product, 0)
	for _, product := range r.state.products {
		if product.Availability == catalog.AvailabilityArchived {
			continue
		}
		if filter.OnlyAvailable && product.Availability != catalog.AvailabilityAvailable {
			continue
		}
		if !matchesCategories(product.CategoryIDs, filter.CategoryIDs) {
			continue
		}
		result = append(result, cloneProduct(product))
	}
	sortProducts(result)
	if len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (r *memoryCatalogRepository) GetProduct(_ context.Context, productID string) (*catalog.Product, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	product, exists := r.state.products[productID]
	if !exists || product.Availability == catalog.AvailabilityArchived {
		return nil, apperror.ErrNotFound
	}
	result := cloneProduct(product)
	return &result, nil
}

func (r *memoryCatalogRepository) ListCategories(context.Context) ([]catalog.Category, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	result := make([]catalog.Category, 0, len(r.state.categories))
	for _, category := range r.state.categories {
		result = append(result, category)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].SortOrder == result[j].SortOrder {
			return result[i].ID < result[j].ID
		}
		return result[i].SortOrder < result[j].SortOrder
	})
	return result, nil
}

func (r *memoryCatalogRepository) ListAdminProducts(_ context.Context, filter catalog.AdminListFilter) ([]catalog.Product, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	result := make([]catalog.Product, 0)
	for _, product := range r.state.products {
		if !matchesCategories(product.CategoryIDs, filter.CategoryIDs) || !matchesAvailability(product.Availability, filter.Availabilities) {
			continue
		}
		result = append(result, cloneProduct(product))
	}
	sortProducts(result)
	if len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (r *memoryCatalogRepository) GetAdminProduct(_ context.Context, productID string) (*catalog.Product, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	product, exists := r.state.products[productID]
	if !exists {
		return nil, apperror.ErrNotFound
	}
	result := cloneProduct(product)
	return &result, nil
}

func (r *memoryCatalogRepository) CreateProduct(_ context.Context, product catalog.Product) (*catalog.Product, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	for _, categoryID := range product.CategoryIDs {
		if _, exists := r.state.categories[categoryID]; !exists {
			return nil, apperror.ErrPrecondition
		}
	}
	product.CreatedAt = r.clock.Now()
	product.UpdatedAt = r.clock.Now()
	r.state.products[product.ID] = cloneProduct(product)
	result := cloneProduct(product)
	return &result, nil
}

func (r *memoryCatalogRepository) UpdateProduct(_ context.Context, productID string, version uint64, patch catalog.ProductPatch) (*catalog.Product, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	product, exists := r.state.products[productID]
	if !exists {
		return nil, apperror.ErrNotFound
	}
	if product.Version != version {
		return nil, apperror.ErrConflict
	}
	for _, path := range patch.Paths {
		switch path {
		case "name":
			product.Name = patch.Name
		case "description":
			product.Description = patch.Description
		case "price":
			product.Price = *patch.Price
		case "availability":
			product.Availability = patch.Availability
		case "category_ids":
			for _, categoryID := range patch.CategoryIDs {
				if _, exists := r.state.categories[categoryID]; !exists {
					return nil, apperror.ErrPrecondition
				}
			}
			product.CategoryIDs = append([]string(nil), patch.CategoryIDs...)
		}
	}
	product.Version++
	product.UpdatedAt = r.clock.Now()
	r.state.products[productID] = cloneProduct(product)
	result := cloneProduct(product)
	return &result, nil
}

func (r *memoryCatalogRepository) ArchiveProduct(_ context.Context, productID string, version uint64) (*catalog.Product, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	product, exists := r.state.products[productID]
	if !exists {
		return nil, apperror.ErrNotFound
	}
	if product.Version != version {
		return nil, apperror.ErrConflict
	}
	product.Availability = catalog.AvailabilityArchived
	product.Version++
	product.UpdatedAt = r.clock.Now()
	r.state.products[productID] = cloneProduct(product)
	result := cloneProduct(product)
	return &result, nil
}

func (r *memoryCatalogRepository) GetCategory(_ context.Context, categoryID string) (*catalog.Category, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	category, exists := r.state.categories[categoryID]
	if !exists {
		return nil, apperror.ErrNotFound
	}
	return &category, nil
}

func (r *memoryCatalogRepository) CreateCategory(_ context.Context, category catalog.Category) (*catalog.Category, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	if category.ParentID != "" {
		if _, exists := r.state.categories[category.ParentID]; !exists {
			return nil, apperror.ErrPrecondition
		}
	}
	category.CreatedAt = r.clock.Now()
	category.UpdatedAt = r.clock.Now()
	r.state.categories[category.ID] = category
	return &category, nil
}

func (r *memoryCatalogRepository) UpdateCategory(_ context.Context, categoryID string, version uint64, patch catalog.CategoryPatch) (*catalog.Category, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	category, exists := r.state.categories[categoryID]
	if !exists {
		return nil, apperror.ErrNotFound
	}
	if category.Version != version {
		return nil, apperror.ErrConflict
	}
	for _, path := range patch.Paths {
		switch path {
		case "name":
			category.Name = patch.Name
		case "parent_id":
			category.ParentID = patch.ParentID
		case "sort_order":
			category.SortOrder = patch.SortOrder
		}
	}
	category.Version++
	category.UpdatedAt = r.clock.Now()
	r.state.categories[categoryID] = category
	return &category, nil
}

func (r *memoryCatalogRepository) DeleteCategory(_ context.Context, categoryID string, version uint64) error {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	category, exists := r.state.categories[categoryID]
	if !exists {
		return apperror.ErrNotFound
	}
	if category.Version != version {
		return apperror.ErrConflict
	}
	for _, current := range r.state.categories {
		if current.ParentID == categoryID {
			return apperror.ErrPrecondition
		}
	}
	for _, product := range r.state.products {
		if matchesCategories(product.CategoryIDs, []string{categoryID}) {
			return apperror.ErrPrecondition
		}
	}
	delete(r.state.categories, categoryID)
	return nil
}

type memoryCartRepository struct {
	state *memoryState
	clock fixedClock
}

var _ cart.Repository = (*memoryCartRepository)(nil)

func (r *memoryCartRepository) Create(_ context.Context, value cart.Cart, tokenHash []byte) error {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	r.state.carts[value.ID] = cartRecord{cart: value, tokenHash: append([]byte(nil), tokenHash...)}
	return nil
}

func (r *memoryCartRepository) Get(_ context.Context, cartID string, tokenHash []byte) (*cart.Cart, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	return r.cartLocked(cartID, tokenHash)
}

func (r *memoryCartRepository) AddItem(_ context.Context, cartID string, tokenHash []byte, itemID, productID string, quantity uint32, expectedVersion uint64) (*cart.Cart, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	record, err := r.mutableCartLocked(cartID, tokenHash, expectedVersion)
	if err != nil {
		return nil, err
	}
	product, exists := r.state.products[productID]
	if !exists {
		return nil, apperror.ErrNotFound
	}
	for index := range record.cart.Items {
		if record.cart.Items[index].ProductID == productID {
			record.cart.Items[index].Quantity += quantity
			record.cart.Version++
			record.cart.UpdatedAt = r.clock.Now()
			r.state.carts[cartID] = *record
			return r.cartViewLocked(*record)
		}
	}
	record.cart.Items = append(record.cart.Items, cart.Item{ID: itemID, ProductID: product.ID, Quantity: quantity})
	record.cart.Version++
	record.cart.UpdatedAt = r.clock.Now()
	r.state.carts[cartID] = *record
	return r.cartViewLocked(*record)
}

func (r *memoryCartRepository) UpdateItem(_ context.Context, cartID string, tokenHash []byte, itemID string, quantity uint32, expectedVersion uint64) (*cart.Cart, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	record, err := r.mutableCartLocked(cartID, tokenHash, expectedVersion)
	if err != nil {
		return nil, err
	}
	found := false
	for index := range record.cart.Items {
		if record.cart.Items[index].ID == itemID {
			record.cart.Items[index].Quantity = quantity
			found = true
		}
	}
	if !found {
		return nil, apperror.ErrNotFound
	}
	record.cart.Version++
	r.state.carts[cartID] = *record
	return r.cartViewLocked(*record)
}

func (r *memoryCartRepository) RemoveItem(_ context.Context, cartID string, tokenHash []byte, itemID string, expectedVersion uint64) (*cart.Cart, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	record, err := r.mutableCartLocked(cartID, tokenHash, expectedVersion)
	if err != nil {
		return nil, err
	}
	for index, item := range record.cart.Items {
		if item.ID == itemID {
			record.cart.Items = append(record.cart.Items[:index], record.cart.Items[index+1:]...)
			record.cart.Version++
			r.state.carts[cartID] = *record
			return r.cartViewLocked(*record)
		}
	}
	return nil, apperror.ErrNotFound
}

func (r *memoryCartRepository) Clear(_ context.Context, cartID string, tokenHash []byte, expectedVersion uint64) (*cart.Cart, error) {
	r.state.mutex.Lock()
	defer r.state.mutex.Unlock()
	record, err := r.mutableCartLocked(cartID, tokenHash, expectedVersion)
	if err != nil {
		return nil, err
	}
	record.cart.Items = nil
	record.cart.Version++
	r.state.carts[cartID] = *record
	return r.cartViewLocked(*record)
}

func (r *memoryCartRepository) mutableCartLocked(cartID string, tokenHash []byte, expectedVersion uint64) (*cartRecord, error) {
	record, exists := r.state.carts[cartID]
	if !exists || !bytes.Equal(record.tokenHash, tokenHash) {
		return nil, apperror.ErrUnauthenticated
	}
	if record.cart.Status != cart.StatusOpen {
		return nil, apperror.ErrPrecondition
	}
	if expectedVersion != 0 && expectedVersion != record.cart.Version {
		return nil, apperror.ErrConflict
	}
	return &record, nil
}

func (r *memoryCartRepository) cartLocked(cartID string, tokenHash []byte) (*cart.Cart, error) {
	record, exists := r.state.carts[cartID]
	if !exists || !bytes.Equal(record.tokenHash, tokenHash) {
		return nil, apperror.ErrUnauthenticated
	}
	return r.cartViewLocked(record)
}

func (r *memoryCartRepository) cartViewLocked(record cartRecord) (*cart.Cart, error) {
	result := record.cart
	result.Items = make([]cart.Item, 0, len(record.cart.Items))
	total, _ := money.New(0, "RUB")
	for _, storedItem := range record.cart.Items {
		product := r.state.products[storedItem.ProductID]
		lineTotal, err := product.Price.Multiply(storedItem.Quantity)
		if err != nil {
			return nil, err
		}
		total, err = total.Add(lineTotal)
		if err != nil {
			return nil, err
		}
		storedItem.ProductName = product.Name
		storedItem.UnitPrice = product.Price
		storedItem.LineTotal = lineTotal
		storedItem.Availability = product.Availability
		result.Items = append(result.Items, storedItem)
	}
	result.Total = total
	return &result, nil
}

func matchesCategories(productCategories, requested []string) bool {
	if len(requested) == 0 {
		return true
	}
	for _, requestedID := range requested {
		for _, productID := range productCategories {
			if requestedID == productID {
				return true
			}
		}
	}
	return false
}

func matchesAvailability(value catalog.Availability, requested []catalog.Availability) bool {
	if len(requested) == 0 {
		return true
	}
	for _, current := range requested {
		if current == value {
			return true
		}
	}
	return false
}

func cloneProduct(product catalog.Product) catalog.Product {
	product.CategoryIDs = append([]string(nil), product.CategoryIDs...)
	return product
}

func sortProducts(products []catalog.Product) {
	sort.Slice(products, func(i, j int) bool {
		if products[i].CreatedAt.Equal(products[j].CreatedAt) {
			return products[i].ID > products[j].ID
		}
		return products[i].CreatedAt.After(products[j].CreatedAt)
	})
}

func formatUUID(value uint64) string {
	return fmt.Sprintf("50000000-0000-4000-8000-%012x", value)
}

func tokenFor(value uint64) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("token-%d", value)))
	return hex.EncodeToString(hash[:])
}
