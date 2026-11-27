package catalog

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/money"
)

func TestAdminServiceCreatesProduct(t *testing.T) {
	repository := &adminRepositoryStub{}
	service := NewAdminService(repository, adminIDGeneratorStub{id: "20000000-0000-4000-8000-000000000010"})
	price, err := money.New(125_000, "RUB")
	require.NoError(t, err)

	result, err := service.CreateProduct(context.Background(), ProductPatch{
		Name:         "  Товар  ",
		Description:  " Описание ",
		Price:        &price,
		Availability: AvailabilityAvailable,
		CategoryIDs:  []string{"10000000-0000-4000-8000-000000000001"},
	})

	require.NoError(t, err)
	require.Equal(t, "20000000-0000-4000-8000-000000000010", result.ID)
	require.Equal(t, "Товар", repository.createdProduct.Name)
	require.Equal(t, "Описание", repository.createdProduct.Description)
}

func TestAdminServiceStoresOrderLimits(t *testing.T) {
	repository := &adminRepositoryStub{}
	service := NewAdminService(repository, adminIDGeneratorStub{id: "20000000-0000-4000-8000-000000000010"})
	price, err := money.New(100_000, "RUB")
	require.NoError(t, err)
	minimum, err := money.New(300_000, "RUB")
	require.NoError(t, err)

	_, err = service.CreateProduct(context.Background(), ProductPatch{
		Name: "Картофель", Description: "", Price: &price, Availability: AvailabilityAvailable,
		UnitOfMeasure: "  10 штук ", MinOrderQuantity: 10, MinOrderAmount: &minimum,
	})

	require.NoError(t, err)
	require.Equal(t, "10 штук", repository.createdProduct.UnitOfMeasure)
	require.Equal(t, uint32(10), repository.createdProduct.MinOrderQuantity)
	require.Equal(t, &minimum, repository.createdProduct.MinOrderAmount)
}

func TestAdminServiceClearsZeroMinimumAmount(t *testing.T) {
	repository := &adminRepositoryStub{product: &Product{
		ID: "20000000-0000-4000-8000-000000000010", Version: 2, Availability: AvailabilityAvailable,
	}}
	service := NewAdminService(repository, adminIDGeneratorStub{})
	price, err := money.New(100_000, "RUB")
	require.NoError(t, err)
	zero, err := money.New(0, "RUB")
	require.NoError(t, err)

	_, err = service.CreateProduct(context.Background(), ProductPatch{
		Name: "Хлеб", Price: &price, Availability: AvailabilityAvailable, MinOrderAmount: &zero,
	})
	require.NoError(t, err)
	require.Nil(t, repository.createdProduct.MinOrderAmount)

	_, err = service.UpdateProduct(context.Background(), repository.product.ID, 2, ProductPatch{
		MinOrderAmount: &zero, Paths: []string{"min_order_amount"},
	})
	require.NoError(t, err)
	require.Nil(t, repository.updatedPatch.MinOrderAmount)
}

func TestAdminServiceRejectsOrderLimits(t *testing.T) {
	service := NewAdminService(&adminRepositoryStub{}, adminIDGeneratorStub{id: "20000000-0000-4000-8000-000000000010"})
	price, err := money.New(100_000, "RUB")
	require.NoError(t, err)
	base := ProductPatch{Name: "Товар", Price: &price, Availability: AvailabilityAvailable}

	tooLong := base
	tooLong.UnitOfMeasure = strings.Repeat("a", 65)
	_, err = service.CreateProduct(context.Background(), tooLong)
	require.Error(t, err)

	tooMany := base
	tooMany.MinOrderQuantity = 10_001
	_, err = service.CreateProduct(context.Background(), tooMany)
	require.Error(t, err)

	usd, err := money.New(100, "USD")
	require.NoError(t, err)
	wrongCurrency := base
	wrongCurrency.MinOrderAmount = &usd
	_, err = service.CreateProduct(context.Background(), wrongCurrency)
	require.Error(t, err)
}

func TestAdminServiceRejectsProductWithoutPrice(t *testing.T) {
	service := NewAdminService(&adminRepositoryStub{}, adminIDGeneratorStub{id: "20000000-0000-4000-8000-000000000010"})

	_, err := service.CreateProduct(context.Background(), ProductPatch{
		Name:         "Товар",
		Availability: AvailabilityAvailable,
	})

	require.Error(t, err)
}

func TestAdminServiceArchivesProduct(t *testing.T) {
	repository := &adminRepositoryStub{product: &Product{
		ID: "20000000-0000-4000-8000-000000000010", Version: 4, Availability: AvailabilityAvailable,
	}}
	service := NewAdminService(repository, adminIDGeneratorStub{})

	result, err := service.DeleteProduct(context.Background(), repository.product.ID, 4)

	require.NoError(t, err)
	require.Equal(t, AvailabilityArchived, result.Availability)
	require.Equal(t, uint64(4), repository.archivedVersion)
}

func TestAdminServiceRejectsStaleProductVersion(t *testing.T) {
	repository := &adminRepositoryStub{product: &Product{
		ID: "20000000-0000-4000-8000-000000000010", Version: 4, Availability: AvailabilityAvailable,
	}}
	service := NewAdminService(repository, adminIDGeneratorStub{})

	_, err := service.DeleteProduct(context.Background(), repository.product.ID, 3)

	require.ErrorIs(t, err, apperror.ErrConflict)
}

type adminRepositoryStub struct {
	product         *Product
	category        *Category
	createdProduct  Product
	updatedPatch    ProductPatch
	archivedVersion uint64
}

func (s *adminRepositoryStub) ListAdminProducts(context.Context, AdminListFilter) ([]Product, error) {
	return nil, nil
}

func (s *adminRepositoryStub) GetAdminProduct(context.Context, string) (*Product, error) {
	return s.product, nil
}

func (s *adminRepositoryStub) CreateProduct(_ context.Context, product Product) (*Product, error) {
	s.createdProduct = product
	return &product, nil
}

func (s *adminRepositoryStub) UpdateProduct(_ context.Context, _ string, _ uint64, patch ProductPatch) (*Product, error) {
	s.updatedPatch = patch
	return s.product, nil
}

func (s *adminRepositoryStub) ArchiveProduct(_ context.Context, _ string, version uint64) (*Product, error) {
	s.archivedVersion = version
	result := *s.product
	result.Availability = AvailabilityArchived
	result.Version++
	return &result, nil
}

func (s *adminRepositoryStub) ListCategories(context.Context) ([]Category, error) {
	return nil, nil
}

func (s *adminRepositoryStub) GetCategory(context.Context, string) (*Category, error) {
	return s.category, nil
}

func (s *adminRepositoryStub) CreateCategory(_ context.Context, category Category) (*Category, error) {
	return &category, nil
}

func (s *adminRepositoryStub) UpdateCategory(context.Context, string, uint64, CategoryPatch) (*Category, error) {
	return s.category, nil
}

func (s *adminRepositoryStub) DeleteCategory(context.Context, string, uint64) error {
	return nil
}

func (s *adminRepositoryStub) ApplyImport(context.Context, *SeedDocument) (*ImportResult, error) {
	return &ImportResult{}, nil
}

type adminIDGeneratorStub struct {
	id string
}

func (s adminIDGeneratorStub) UUID() (string, error) {
	return s.id, nil
}
