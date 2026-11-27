package catalog

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/SV1Stail/online_market/internal/apperror"
	"github.com/SV1Stail/online_market/internal/identifier"
	"github.com/SV1Stail/online_market/internal/money"
)

const maxCatalogSeedBytes = 1 << 20

// catalogSeedNamespace is the UUID v5 namespace for ids derived from a catalog key.
// The name hashed with this namespace is "category:"+key or "product:"+key.
// Changing either value changes ids of rows that do not set id explicitly.
var catalogSeedNamespace = [16]byte{
	0xf0, 0xe1, 0xd2, 0xc3, 0xb4, 0xa5, 0x46, 0x78,
	0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67,
}

var catalogKeyPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// SeedDocument is a catalog file with a field mask on every entity.
// Paths records which YAML fields were present so a later update can
// overwrite only those columns.
type SeedDocument struct {
	Categories []SeedCategory
	Products   []SeedProduct
}

type SeedCategory struct {
	Key   string
	ID    string
	Patch CategoryPatch
}

type SeedProduct struct {
	Key   string
	ID    string
	Patch ProductPatch
}

type requiredFieldError struct {
	Entity string
	Key    string
	Field  string
	Path   string
}

func (e *requiredFieldError) Error() string {
	return e.Path + ": is required"
}

func (e *requiredFieldError) Unwrap() error {
	return &apperror.ValidationError{Field: e.Path, Message: "is required"}
}

// ParseSeedFile reads a catalog YAML file. The file must be at most 1 MiB.
func ParseSeedFile(path string) (*SeedDocument, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open catalog seed file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCatalogSeedBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read catalog seed file: %w", err)
	}
	if len(data) > maxCatalogSeedBytes {
		return nil, fmt.Errorf("catalog seed file must not exceed 1 MiB")
	}
	return ParseSeed(data)
}

// ParseSeed validates a catalog document for a clean database.
// Every row must contain the NOT NULL columns that have no database default.
// Field masks still omit columns the file did not mention.
func ParseSeed(data []byte) (*SeedDocument, error) {
	return parseCatalogDocument(data, true)
}

// ParseCatalogFile validates a catalog document for an administrator import.
// A row that already exists may omit NOT NULL columns; those columns stay unchanged.
func ParseCatalogFile(data []byte) (*SeedDocument, error) {
	return parseCatalogDocument(data, false)
}

func parseCatalogDocument(data []byte, requireCreates bool) (*SeedDocument, error) {
	if len(data) > maxCatalogSeedBytes {
		return nil, fmt.Errorf("catalog seed file must not exceed 1 MiB")
	}
	root, err := seedRoot(data)
	if err != nil {
		return nil, err
	}
	fields, err := mappingFields(root)
	if err != nil {
		return nil, err
	}
	var errs []error
	for name := range fields {
		if name != "categories" && name != "products" {
			errs = append(errs, apperror.Validation(name, "is not a supported field"))
		}
	}
	seenIDs := make(map[string]struct{})
	categories, categoryErrs := parseSeedCategories(fields["categories"], seenIDs, requireCreates)
	products, productErrs := parseSeedProducts(fields["products"], seenIDs, requireCreates)
	errs = append(errs, categoryErrs...)
	errs = append(errs, productErrs...)
	errs = append(errs, linkSeedReferences(categories, products)...)
	if err := joinSeedErrors(errs); err != nil {
		return nil, err
	}
	return &SeedDocument{Categories: categories, Products: products}, nil
}

// LogSeedError writes one error line for each empty NOT NULL field.
// Other seed failures are logged once, without the file body.
func LogSeedError(logger *slog.Logger, err error) {
	if logger == nil || err == nil {
		return
	}
	var others []string
	for _, item := range flattenSeedErrors(err) {
		var required *requiredFieldError
		if errors.As(item, &required) {
			logger.Error(
				"catalog seed required field is empty",
				"entity", required.Entity,
				"key", required.Key,
				"field", required.Field,
			)
			continue
		}
		others = append(others, item.Error())
	}
	if len(others) > 0 {
		logger.Error("catalog file rejected", "error", strings.Join(others, "; "))
	}
}

func (item SeedCategory) stored() Category {
	row := Category{ID: item.ID, Version: 1}
	if containsPath(item.Patch.Paths, "name") {
		row.Name = item.Patch.Name
	}
	if containsPath(item.Patch.Paths, "parent_id") {
		row.ParentID = item.Patch.ParentID
	}
	if containsPath(item.Patch.Paths, "sort_order") {
		row.SortOrder = item.Patch.SortOrder
	}
	return row
}

func (item SeedProduct) stored() Product {
	row := Product{ID: item.ID, Version: 1}
	if containsPath(item.Patch.Paths, "name") {
		row.Name = item.Patch.Name
	}
	if containsPath(item.Patch.Paths, "description") {
		row.Description = item.Patch.Description
	}
	if containsPath(item.Patch.Paths, "price") && item.Patch.Price != nil {
		row.Price = *item.Patch.Price
	}
	if containsPath(item.Patch.Paths, "availability") {
		row.Availability = item.Patch.Availability
	}
	if containsPath(item.Patch.Paths, "category_ids") {
		row.CategoryIDs = append([]string(nil), item.Patch.CategoryIDs...)
	}
	if containsPath(item.Patch.Paths, "unit_of_measure") {
		row.UnitOfMeasure = item.Patch.UnitOfMeasure
	}
	if containsPath(item.Patch.Paths, "min_order_quantity") {
		row.MinOrderQuantity = item.Patch.MinOrderQuantity
	}
	if containsPath(item.Patch.Paths, "min_order_amount") {
		row.MinOrderAmount = item.Patch.MinOrderAmount
	}
	return row
}

func seedRoot(data []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, apperror.Validation("file", "must contain a mapping")
		}
		return nil, fmt.Errorf("catalog seed file is not valid YAML: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, apperror.Validation("file", "must contain one document")
		}
		return nil, fmt.Errorf("catalog seed file is not valid YAML: %w", err)
	}
	if root.Kind == yaml.DocumentNode {
		if len(root.Content) != 1 {
			return nil, apperror.Validation("file", "must contain a mapping")
		}
		root = *root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return nil, apperror.Validation("file", "must contain a mapping")
	}
	return &root, nil
}

func parseSeedCategories(node *yaml.Node, seenIDs map[string]struct{}, requireCreates bool) ([]SeedCategory, []error) {
	if node == nil {
		return nil, nil
	}
	items, err := sequenceItems(node, "categories")
	if err != nil {
		return nil, []error{err}
	}
	categories := make([]SeedCategory, 0, len(items))
	var errs []error
	seenKeys := make(map[string]struct{}, len(items))
	for index, item := range items {
		category, itemErrs := parseSeedCategory(index, item, seenKeys, seenIDs, requireCreates)
		errs = append(errs, itemErrs...)
		categories = append(categories, category)
	}
	return categories, errs
}

func parseSeedCategory(index int, node *yaml.Node, seenKeys, seenIDs map[string]struct{}, requireCreates bool) (SeedCategory, []error) {
	path := fmt.Sprintf("categories[%d]", index)
	fields, err := mappingFields(node)
	if err != nil {
		return SeedCategory{}, []error{seedMappingError(path, err)}
	}
	var errs []error
	for name := range fields {
		switch name {
		case "key", "id", "name", "parent", "sort_order":
		default:
			errs = append(errs, apperror.Validation(path+"."+name, "is not a supported field"))
		}
	}
	key, keyErrs := parseSeedKey(path, "category", fields["key"])
	errs = append(errs, keyErrs...)
	if key != "" {
		if _, exists := seenKeys[key]; exists {
			errs = append(errs, apperror.Validation(path+".key", "must be unique"))
		}
		seenKeys[key] = struct{}{}
	}
	id, idErrs := parseSeedID(path, "category", key, fields["id"], seenIDs)
	errs = append(errs, idErrs...)

	category := SeedCategory{Key: key, ID: id}
	name, nameSet, nameErr := optionalString(fields["name"], path+".name")
	if nameErr != nil {
		errs = append(errs, nameErr)
	} else if err := requireSeedText("category", key, "name", path+".name", name, nameSet, requireCreates); err != nil {
		errs = append(errs, err)
	} else if nameSet {
		category.Patch.Name = strings.TrimSpace(name)
		category.Patch.Paths = append(category.Patch.Paths, "name")
	}

	if parent, ok := fields["parent"]; ok {
		parentKey, parentErr := parseParentKey(parent, path+".parent")
		if parentErr != nil {
			errs = append(errs, parentErr)
		} else {
			category.Patch.ParentID = parentKey
			category.Patch.Paths = append(category.Patch.Paths, "parent_id")
		}
	}

	if sortNode, ok := fields["sort_order"]; ok {
		sortOrder, sortErr := parseSortOrder(sortNode, path+".sort_order")
		if sortErr != nil {
			errs = append(errs, sortErr)
		} else {
			category.Patch.SortOrder = sortOrder
			category.Patch.Paths = append(category.Patch.Paths, "sort_order")
		}
	}
	if len(category.Patch.Paths) > 0 {
		category.Patch = normalizeCategoryPatch(category.Patch)
	}
	return category, errs
}

func parseSeedProducts(node *yaml.Node, seenIDs map[string]struct{}, requireCreates bool) ([]SeedProduct, []error) {
	if node == nil {
		return nil, nil
	}
	items, err := sequenceItems(node, "products")
	if err != nil {
		return nil, []error{err}
	}
	products := make([]SeedProduct, 0, len(items))
	var errs []error
	seenKeys := make(map[string]struct{}, len(items))
	for index, item := range items {
		product, itemErrs := parseSeedProduct(index, item, seenKeys, seenIDs, requireCreates)
		errs = append(errs, itemErrs...)
		products = append(products, product)
	}
	return products, errs
}

func parseSeedProduct(index int, node *yaml.Node, seenKeys, seenIDs map[string]struct{}, requireCreates bool) (SeedProduct, []error) {
	path := fmt.Sprintf("products[%d]", index)
	fields, err := mappingFields(node)
	if err != nil {
		return SeedProduct{}, []error{seedMappingError(path, err)}
	}
	var errs []error
	for name := range fields {
		switch name {
		case "key", "id", "name", "description", "price_minor", "currency", "unit",
			"min_order_quantity", "min_order_amount_minor", "availability", "categories":
		default:
			errs = append(errs, apperror.Validation(path+"."+name, "is not a supported field"))
		}
	}
	key, keyErrs := parseSeedKey(path, "product", fields["key"])
	errs = append(errs, keyErrs...)
	if key != "" {
		if _, exists := seenKeys[key]; exists {
			errs = append(errs, apperror.Validation(path+".key", "must be unique"))
		}
		seenKeys[key] = struct{}{}
	}
	id, idErrs := parseSeedID(path, "product", key, fields["id"], seenIDs)
	errs = append(errs, idErrs...)

	product := SeedProduct{Key: key, ID: id}
	name, nameSet, nameErr := optionalString(fields["name"], path+".name")
	if nameErr != nil {
		errs = append(errs, nameErr)
	} else if err := requireSeedText("product", key, "name", path+".name", name, nameSet, requireCreates); err != nil {
		errs = append(errs, err)
	} else if nameSet {
		product.Patch.Name = strings.TrimSpace(name)
		product.Patch.Paths = append(product.Patch.Paths, "name")
	}

	if node, ok := fields["description"]; ok {
		description, descriptionErr := requiredString(node, path+".description")
		if descriptionErr != nil {
			errs = append(errs, descriptionErr)
		} else {
			product.Patch.Description = strings.TrimSpace(description)
			product.Patch.Paths = append(product.Patch.Paths, "description")
		}
	}
	priceErrs := parseSeedPrice(path, fields, &product, requireCreates)
	errs = append(errs, priceErrs...)
	if node, ok := fields["unit"]; ok {
		unit, unitErr := requiredString(node, path+".unit")
		if unitErr != nil {
			errs = append(errs, unitErr)
		} else {
			product.Patch.UnitOfMeasure = strings.TrimSpace(unit)
			product.Patch.Paths = append(product.Patch.Paths, "unit_of_measure")
		}
	}
	if node, ok := fields["min_order_quantity"]; ok {
		quantity, quantityErr := parseNonNegativeInt(node, path+".min_order_quantity", maxMinOrderQuantity)
		if quantityErr != nil {
			errs = append(errs, quantityErr)
		} else {
			product.Patch.MinOrderQuantity = uint32(quantity)
			product.Patch.Paths = append(product.Patch.Paths, "min_order_quantity")
		}
	}
	if node, ok := fields["min_order_amount_minor"]; ok {
		amountErr := parseMinOrderAmount(node, path+".min_order_amount_minor", &product)
		if amountErr != nil {
			errs = append(errs, amountErr)
		}
	}
	availability, availabilitySet, availabilityErr := optionalString(fields["availability"], path+".availability")
	if availabilityErr != nil {
		errs = append(errs, availabilityErr)
	} else if err := requireSeedText("product", key, "availability", path+".availability", availability, availabilitySet, requireCreates); err != nil {
		errs = append(errs, err)
	} else if availabilitySet {
		product.Patch.Availability = Availability(strings.TrimSpace(availability))
		product.Patch.Paths = append(product.Patch.Paths, "availability")
	}
	if node, ok := fields["categories"]; ok {
		keys, listErr := parseKeyList(node, path+".categories")
		if listErr != nil {
			errs = append(errs, listErr)
		} else {
			product.Patch.CategoryIDs = keys
			product.Patch.Paths = append(product.Patch.Paths, "category_ids")
		}
	}
	if len(product.Patch.Paths) > 0 {
		product.Patch = normalizeProductPatch(product.Patch)
		// Category keys are resolved after the whole file is read.
		paths := make([]string, 0, len(product.Patch.Paths))
		for _, item := range product.Patch.Paths {
			if item != "category_ids" {
				paths = append(paths, item)
			}
		}
		if len(paths) > 0 {
			if err := validateProductPatch(product.Patch, paths); err != nil {
				errs = append(errs, remapSeedValidation(err, "products", index))
			}
		}
	}
	return product, errs
}

func parseSeedPrice(path string, fields map[string]*yaml.Node, product *SeedProduct, requireCreates bool) []error {
	_, hasMinor := fields["price_minor"]
	_, hasCurrency := fields["currency"]
	if !hasMinor && !hasCurrency {
		if !requireCreates {
			return nil
		}
		return []error{
			requiredSeedField("product", product.Key, "price_minor", path+".price_minor"),
			requiredSeedField("product", product.Key, "currency", path+".currency"),
		}
	}
	var errs []error
	var minor int64
	var currency string
	minorOK := false
	currencyOK := false
	if !hasMinor {
		errs = append(errs, requiredSeedField("product", product.Key, "price_minor", path+".price_minor"))
	} else if value, err := parseInt(fields["price_minor"], path+".price_minor"); err != nil {
		errs = append(errs, err)
	} else if value < 0 {
		errs = append(errs, apperror.Validation(path+".price_minor", "must not be negative"))
	} else {
		minor = value
		minorOK = true
	}
	if !hasCurrency {
		errs = append(errs, requiredSeedField("product", product.Key, "currency", path+".currency"))
	} else if value, err := requiredString(fields["currency"], path+".currency"); err != nil {
		errs = append(errs, err)
	} else if strings.TrimSpace(value) == "" {
		errs = append(errs, requiredSeedField("product", product.Key, "currency", path+".currency"))
	} else {
		currency = strings.TrimSpace(value)
		currencyOK = true
	}
	if minorOK && currencyOK {
		amount, err := money.New(minor, currency)
		if err != nil {
			errs = append(errs, apperror.Validation(path+".currency", "must be an uppercase ISO 4217 code"))
		} else {
			product.Patch.Price = &amount
			product.Patch.Paths = append(product.Patch.Paths, "price")
		}
	}
	return errs
}

func parseMinOrderAmount(node *yaml.Node, path string, product *SeedProduct) error {
	if yamlNull(node) {
		product.Patch.Paths = append(product.Patch.Paths, "min_order_amount")
		return nil
	}
	value, err := parseInt(node, path)
	if err != nil {
		return err
	}
	if value < 0 {
		return apperror.Validation(path, "must not be negative")
	}
	if value == 0 {
		product.Patch.Paths = append(product.Patch.Paths, "min_order_amount")
		return nil
	}
	amount, err := money.New(value, "RUB")
	if err != nil {
		return apperror.Validation(path, "must contain a positive RUB amount")
	}
	product.Patch.MinOrderAmount = &amount
	product.Patch.Paths = append(product.Patch.Paths, "min_order_amount")
	return nil
}

func linkSeedReferences(categories []SeedCategory, products []SeedProduct) []error {
	var errs []error
	for index, category := range categories {
		if !containsPath(category.Patch.Paths, "parent_id") || category.Patch.ParentID == "" {
			if err := validateLinkedCategory(index, categories[index]); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		parentID, ok := lookupCategoryID(categories, category.Patch.ParentID)
		path := fmt.Sprintf("categories[%d].parent", index)
		if !ok {
			errs = append(errs, apperror.Validation(path, "references an unknown key"))
		} else if parentID == category.ID {
			errs = append(errs, apperror.Validation(path, "must not reference the category itself"))
		} else {
			categories[index].Patch.ParentID = parentID
		}
		if err := validateLinkedCategory(index, categories[index]); err != nil {
			errs = append(errs, err)
		}
	}
	if _, err := orderSeedCategories(categories); err != nil {
		errs = append(errs, err)
	}
	for index, product := range products {
		if !containsPath(product.Patch.Paths, "category_ids") {
			continue
		}
		ids := make([]string, 0, len(product.Patch.CategoryIDs))
		path := fmt.Sprintf("products[%d].categories", index)
		valid := true
		for _, key := range product.Patch.CategoryIDs {
			id, ok := lookupCategoryID(categories, key)
			if !ok {
				errs = append(errs, apperror.Validation(path, "references an unknown key"))
				valid = false
				break
			}
			ids = append(ids, id)
		}
		if !valid {
			continue
		}
		products[index].Patch.CategoryIDs = uniqueStrings(ids)
		if err := validateCategoryIDs(products[index].Patch.CategoryIDs); err != nil {
			errs = append(errs, remapSeedValidation(err, "products", index))
		}
	}
	return errs
}

func validateLinkedCategory(index int, category SeedCategory) error {
	paths := make([]string, 0, len(category.Patch.Paths))
	for _, path := range category.Patch.Paths {
		if path == "parent_id" && category.Patch.ParentID != "" && !identifier.IsUUID(category.Patch.ParentID) {
			continue
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return nil
	}
	if err := validateCategoryPatch(category.ID, category.Patch, paths); err != nil {
		return remapSeedValidation(err, "categories", index)
	}
	return nil
}

func lookupCategoryID(categories []SeedCategory, key string) (string, bool) {
	for _, category := range categories {
		if category.Key == key && category.Key != "" {
			return category.ID, true
		}
	}
	return "", false
}

func orderSeedCategories(categories []SeedCategory) ([]SeedCategory, error) {
	pending := make(map[string]SeedCategory, len(categories))
	children := make(map[string][]string)
	var queue []string
	expected := 0
	for _, category := range categories {
		if category.ID == "" {
			continue
		}
		expected++
		pending[category.ID] = category
		parent := ""
		if containsPath(category.Patch.Paths, "parent_id") {
			parent = category.Patch.ParentID
		}
		if parent == "" || !identifier.IsUUID(parent) {
			queue = append(queue, category.ID)
			continue
		}
		children[parent] = append(children[parent], category.ID)
	}
	ordered := make([]SeedCategory, 0, expected)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		category, ok := pending[id]
		if !ok {
			continue
		}
		ordered = append(ordered, category)
		delete(pending, id)
		queue = append(queue, children[id]...)
	}
	if len(pending) > 0 || len(ordered) != expected {
		return nil, apperror.Validation("categories", "must not contain cycles")
	}
	return ordered, nil
}

func parseSeedKey(path, entity string, node *yaml.Node) (string, []error) {
	if node == nil {
		return "", []error{requiredSeedField(entity, "", "key", path+".key")}
	}
	if yamlNull(node) {
		return "", []error{requiredSeedField(entity, "", "key", path+".key")}
	}
	value, err := requiredString(node, path+".key")
	if err != nil {
		return "", []error{err}
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", []error{requiredSeedField(entity, "", "key", path+".key")}
	}
	if len(value) > 64 || !catalogKeyPattern.MatchString(value) {
		return "", []error{apperror.Validation(path+".key", "must be a lowercase slug of at most 64 characters")}
	}
	return value, nil
}

func parseSeedID(path, entity, key string, node *yaml.Node, seen map[string]struct{}) (string, []error) {
	id := ""
	if key != "" && node == nil {
		id = idFromCatalogKey(entity, key)
	}
	if node != nil {
		if yamlNull(node) {
			if key == "" {
				return "", []error{requiredSeedField(entity, key, "id", path+".id")}
			}
			id = idFromCatalogKey(entity, key)
		} else {
			value, err := requiredString(node, path+".id")
			if err != nil {
				return "", []error{err}
			}
			id = strings.TrimSpace(value)
			if !identifier.IsUUID(id) {
				return "", []error{apperror.Validation(path+".id", "must be a valid UUID")}
			}
		}
	}
	if id == "" {
		return "", nil
	}
	if _, exists := seen[id]; exists {
		return id, []error{apperror.Validation(path+".id", "must be unique")}
	}
	seen[id] = struct{}{}
	return id, nil
}

func idFromCatalogKey(entity, key string) string {
	digest := sha1.New()
	digest.Write(catalogSeedNamespace[:])
	digest.Write([]byte(entity))
	digest.Write([]byte{0})
	digest.Write([]byte(key))
	sum := digest.Sum(nil)
	var value [16]byte
	copy(value[:], sum[:16])
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func parseParentKey(node *yaml.Node, path string) (string, error) {
	if yamlNull(node) {
		return "", nil
	}
	value, err := requiredString(node, path)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) > 64 || !catalogKeyPattern.MatchString(value) {
		return "", apperror.Validation(path, "must reference a category key")
	}
	return value, nil
}

func parseKeyList(node *yaml.Node, path string) ([]string, error) {
	if node.Kind != yaml.SequenceNode {
		return nil, apperror.Validation(path, "must be a list")
	}
	keys := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if yamlNull(item) {
			return nil, apperror.Validation(path, "must reference a category key")
		}
		value, err := requiredString(item, path)
		if err != nil {
			return nil, err
		}
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 64 || !catalogKeyPattern.MatchString(value) {
			return nil, apperror.Validation(path, "must reference a category key")
		}
		keys = append(keys, value)
	}
	return keys, nil
}

func parseSortOrder(node *yaml.Node, path string) (uint32, error) {
	value, err := parseInt(node, path)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, apperror.Validation(path, "must not be negative")
	}
	if value > math.MaxInt32 {
		return 0, apperror.Validation(path, "must not exceed 2147483647")
	}
	return uint32(value), nil
}

func parseNonNegativeInt(node *yaml.Node, path string, maximum int64) (int64, error) {
	value, err := parseInt(node, path)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, apperror.Validation(path, "must not be negative")
	}
	if value > maximum {
		return 0, apperror.Validation(path, "must not exceed "+strconv.FormatInt(maximum, 10))
	}
	return value, nil
}

func parseInt(node *yaml.Node, path string) (int64, error) {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		return 0, apperror.Validation(path, "must be an integer")
	}
	value, err := strconv.ParseInt(node.Value, 10, 64)
	if err != nil {
		return 0, apperror.Validation(path, "must be an integer")
	}
	return value, nil
}

func optionalString(node *yaml.Node, path string) (string, bool, error) {
	if node == nil {
		return "", false, nil
	}
	if yamlNull(node) {
		return "", false, nil
	}
	value, err := requiredString(node, path)
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func requiredString(node *yaml.Node, path string) (string, error) {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", apperror.Validation(path, "must be a string")
	}
	return node.Value, nil
}

func yamlNull(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!null"
}

func seedMappingError(path string, err error) error {
	var validation *apperror.ValidationError
	if errors.As(err, &validation) {
		if validation.Field == "" {
			return apperror.Validation(path, validation.Message)
		}
		return apperror.Validation(path+"."+validation.Field, validation.Message)
	}
	return apperror.Validation(path, "must be a mapping")
}

func mappingFields(node *yaml.Node) (map[string]*yaml.Node, error) {
	if node == nil || node.Kind != yaml.MappingNode || len(node.Content)%2 != 0 {
		return nil, fmt.Errorf("expected a mapping")
	}
	fields := make(map[string]*yaml.Node, len(node.Content)/2)
	for index := 0; index < len(node.Content); index += 2 {
		keyNode := node.Content[index]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
			return nil, fmt.Errorf("expected a string field name")
		}
		if _, exists := fields[keyNode.Value]; exists {
			return nil, apperror.Validation(keyNode.Value, "must not be repeated")
		}
		fields[keyNode.Value] = node.Content[index+1]
	}
	return fields, nil
}

func sequenceItems(node *yaml.Node, path string) ([]*yaml.Node, error) {
	if yamlNull(node) {
		return nil, nil
	}
	if node.Kind != yaml.SequenceNode {
		return nil, apperror.Validation(path, "must be a list")
	}
	return node.Content, nil
}

func requiredSeedField(entity, key, field, path string) error {
	return &requiredFieldError{Entity: entity, Key: key, Field: field, Path: path}
}

func requireSeedText(entity, key, field, path, value string, present, required bool) error {
	if present && strings.TrimSpace(value) == "" {
		return requiredSeedField(entity, key, field, path)
	}
	if !present && required {
		return requiredSeedField(entity, key, field, path)
	}
	return nil
}

// mergedCategoryCycle checks the parent graph after file values replace stored parents.
// A category omitted from the file keeps the parent currently stored in the database.
func mergedCategoryCycle(categories []SeedCategory, existingParents map[string]string) error {
	parentOf := make(map[string]string, len(existingParents)+len(categories))
	for id, parent := range existingParents {
		parentOf[id] = parent
	}
	for _, category := range categories {
		if category.ID == "" {
			continue
		}
		if _, exists := parentOf[category.ID]; !exists {
			parentOf[category.ID] = ""
		}
		if containsPath(category.Patch.Paths, "parent_id") {
			parentOf[category.ID] = category.Patch.ParentID
		}
	}
	for start := range parentOf {
		seen := make(map[string]struct{})
		for current := start; current != ""; {
			if _, loop := seen[current]; loop {
				return apperror.Validation("categories", "must not contain cycles")
			}
			seen[current] = struct{}{}
			next, ok := parentOf[current]
			if !ok {
				break
			}
			current = next
		}
	}
	return nil
}

func remapSeedValidation(err error, section string, index int) error {
	var validation *apperror.ValidationError
	if !errors.As(err, &validation) {
		return err
	}
	field := strings.TrimPrefix(validation.Field, "patch.")
	switch field {
	case "name", "description", "availability", "min_order_quantity":
	case "parent_id":
		field = "parent"
	case "price":
		field = "price_minor"
	case "unit_of_measure":
		field = "unit"
	case "min_order_amount":
		field = "min_order_amount_minor"
	case "category_ids":
		field = "categories"
	default:
		return apperror.Validation(fmt.Sprintf("%s[%d]", section, index), validation.Message)
	}
	return apperror.Validation(fmt.Sprintf("%s[%d].%s", section, index, field), validation.Message)
}

func joinSeedErrors(errs []error) error {
	filtered := make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			filtered = append(filtered, err)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return errors.Join(filtered...)
}

func flattenSeedErrors(err error) []error {
	if err == nil {
		return nil
	}
	type joined interface{ Unwrap() []error }
	if group, ok := err.(joined); ok {
		var out []error
		for _, item := range group.Unwrap() {
			out = append(out, flattenSeedErrors(item)...)
		}
		return out
	}
	return []error{err}
}
