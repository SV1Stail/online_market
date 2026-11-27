package catalog

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/SV1Stail/online_market/internal/apperror"
)

func TestParseSeedExampleFile(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "..", "..", "seed", "products.yaml"))
	require.NoError(t, err)

	document, err := ParseSeed(data)
	require.NoError(t, err)
	require.Len(t, document.Categories, 2)
	require.Len(t, document.Products, 1)

	electronics := document.Categories[0]
	require.Equal(t, "electronics", electronics.Key)
	require.Equal(t, "10000000-0000-4000-8000-000000000011", electronics.ID)
	require.Equal(t, []string{"name", "parent_id", "sort_order"}, electronics.Patch.Paths)
	require.Equal(t, "Электроника", electronics.stored().Name)
	require.Empty(t, electronics.stored().ParentID)
	require.Equal(t, uint32(10), electronics.stored().SortOrder)

	accessories := document.Categories[1]
	require.Equal(t, "10000000-0000-4000-8000-000000000012", accessories.ID)
	require.Equal(t, electronics.ID, accessories.stored().ParentID)

	product := document.Products[0]
	require.Equal(t, "20000000-0000-4000-8000-000000000011", product.ID)
	require.Equal(t, []string{"electronics", "accessories"}, []string{electronics.Key, accessories.Key})
	require.ElementsMatch(t, []string{
		"name", "description", "price", "unit_of_measure", "min_order_quantity",
		"min_order_amount", "availability", "category_ids",
	}, product.Patch.Paths)
	stored := product.stored()
	require.Equal(t, "Кабель USB-C", stored.Name)
	require.Equal(t, int64(79000), stored.Price.MinorUnits)
	require.Equal(t, "RUB", stored.Price.Currency)
	require.Equal(t, AvailabilityAvailable, stored.Availability)
	require.Equal(t, []string{electronics.ID, accessories.ID}, stored.CategoryIDs)
	require.Equal(t, uint32(1), stored.MinOrderQuantity)
	require.NotNil(t, stored.MinOrderAmount)
	require.Equal(t, int64(79000), stored.MinOrderAmount.MinorUnits)
}

func TestParseSeedOmitsAbsentFieldsAndUsesDefaults(t *testing.T) {
	document, err := ParseSeed([]byte(`
categories:
  - key: root
    name: Корень
products:
  - key: item
    name: Товар
    price_minor: 100
    currency: RUB
    availability: available
`))
	require.NoError(t, err)

	category := document.Categories[0]
	require.Equal(t, []string{"name"}, category.Patch.Paths)
	require.Empty(t, category.stored().ParentID)
	require.Zero(t, category.stored().SortOrder)

	product := document.Products[0].stored()
	require.NotContains(t, document.Products[0].Patch.Paths, "description")
	require.NotContains(t, document.Products[0].Patch.Paths, "unit_of_measure")
	require.NotContains(t, document.Products[0].Patch.Paths, "min_order_quantity")
	require.NotContains(t, document.Products[0].Patch.Paths, "min_order_amount")
	require.NotContains(t, document.Products[0].Patch.Paths, "category_ids")
	require.Empty(t, product.Description)
	require.Empty(t, product.UnitOfMeasure)
	require.Zero(t, product.MinOrderQuantity)
	require.Nil(t, product.MinOrderAmount)
	require.Empty(t, product.CategoryIDs)
}

func TestParseSeedNullParentIsRoot(t *testing.T) {
	document, err := ParseSeed([]byte(`
categories:
  - key: root
    name: Корень
    parent: null
`))
	require.NoError(t, err)
	require.Equal(t, []string{"name", "parent_id"}, document.Categories[0].Patch.Paths)
	require.Empty(t, document.Categories[0].stored().ParentID)
}

func TestParseSeedResolvesParentDeclaredLater(t *testing.T) {
	document, err := ParseSeed([]byte(`
categories:
  - key: child
    name: Дочерняя
    parent: root
  - key: root
    name: Корень
`))
	require.NoError(t, err)
	require.Equal(t, document.Categories[1].ID, document.Categories[0].stored().ParentID)
}

func TestParseSeedReportsEveryEmptyRequiredField(t *testing.T) {
	_, err := ParseSeed([]byte(`
categories:
  - key: root
products:
  - key: item
    price_minor: 10
`))
	require.Error(t, err)
	require.ElementsMatch(t, []string{
		"categories[0].name: is required",
		"products[0].name: is required",
		"products[0].currency: is required",
		"products[0].availability: is required",
	}, validationTexts(err))
}

func TestLogSeedErrorNamesEmptyRequiredField(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	_, err := ParseSeed([]byte("categories:\n  - key: root\nproducts:\n  - key: item\n"))
	require.Error(t, err)

	LogSeedError(logger, err)

	text := buf.String()
	require.Contains(t, text, `"entity":"category"`)
	require.Contains(t, text, `"key":"root"`)
	require.Contains(t, text, `"field":"name"`)
	require.Contains(t, text, `"entity":"product"`)
	require.Contains(t, text, `"field":"price_minor"`)
	require.Contains(t, text, `"field":"currency"`)
	require.Contains(t, text, `"field":"availability"`)
	require.NotContains(t, text, "Корень")
}

func TestParseSeedRejectsUnknownField(t *testing.T) {
	_, err := ParseSeed([]byte(`
categories:
  - key: root
    name: Корень
    color: red
`))
	require.Error(t, err)
	require.Contains(t, validationTexts(err), "categories[0].color: is not a supported field")
}

func TestParseSeedRejectsDuplicateKeyAndID(t *testing.T) {
	_, err := ParseSeed([]byte(`
categories:
  - key: root
    id: 10000000-0000-4000-8000-000000000011
    name: Корень
  - key: root
    id: 10000000-0000-4000-8000-000000000011
    name: Ещё
`))
	require.Error(t, err)
	texts := validationTexts(err)
	require.Contains(t, texts, "categories[1].key: must be unique")
	require.Contains(t, texts, "categories[1].id: must be unique")
}

func TestParseSeedRejectsUnknownReference(t *testing.T) {
	_, err := ParseSeed([]byte(`
categories:
  - key: root
    name: Корень
    parent: missing
products:
  - key: item
    name: Товар
    price_minor: 10
    currency: RUB
    availability: available
    categories: [missing-product]
`))
	require.Error(t, err)
	texts := validationTexts(err)
	require.Contains(t, texts, "categories[0].parent: references an unknown key")
	require.Contains(t, texts, "products[0].categories: references an unknown key")
}

func TestParseSeedRejectsCategoryCycle(t *testing.T) {
	_, err := ParseSeed([]byte(`
categories:
  - key: left
    name: Левая
    parent: right
  - key: right
    name: Правая
    parent: left
`))
	require.Error(t, err)
	require.Contains(t, validationTexts(err), "categories: must not contain cycles")
}

func TestParseSeedRejectsAvailability(t *testing.T) {
	_, err := ParseSeed([]byte(`
products:
  - key: item
    name: Товар
    price_minor: 10
    currency: RUB
    availability: missing
`))
	require.Error(t, err)
	require.Contains(t, validationTexts(err), "products[0].availability: must be specified")
}

func TestParseSeedZeroMinimumAmountIsNull(t *testing.T) {
	document, err := ParseSeed([]byte(`
products:
  - key: item
    name: Товар
    price_minor: 10
    currency: RUB
    availability: available
    min_order_amount_minor: 0
`))
	require.NoError(t, err)
	require.Contains(t, document.Products[0].Patch.Paths, "min_order_amount")
	require.Nil(t, document.Products[0].stored().MinOrderAmount)
}

func TestParseSeedKeyWithoutIDIsStable(t *testing.T) {
	const file = `
categories:
  - key: root
    name: Корень
products:
  - key: item
    name: Товар
    price_minor: 10
    currency: RUB
    availability: available
`
	first, err := ParseSeed([]byte(file))
	require.NoError(t, err)
	second, err := ParseSeed([]byte(file))
	require.NoError(t, err)

	require.Equal(t, first.Categories[0].ID, second.Categories[0].ID)
	require.Equal(t, first.Products[0].ID, second.Products[0].ID)
	require.NotEqual(t, first.Categories[0].ID, first.Products[0].ID)
	require.Equal(t, byte('5'), first.Categories[0].ID[14])
	require.Equal(t, byte('5'), first.Products[0].ID[14])
}

func TestParseSeedRejectsLargeFile(t *testing.T) {
	_, err := ParseSeed(bytes.Repeat([]byte(" "), maxCatalogSeedBytes+1))
	require.EqualError(t, err, "catalog seed file must not exceed 1 MiB")
}

func TestParseCatalogFileAllowsPartialUpdate(t *testing.T) {
	const categoryID = "10000000-0000-4000-8000-000000000011"
	const productID = "20000000-0000-4000-8000-000000000011"
	document, err := ParseCatalogFile([]byte(`
categories:
  - key: root
    id: ` + categoryID + `
    sort_order: 5
products:
  - key: item
    id: ` + productID + `
    description: Только описание
`))
	require.NoError(t, err)
	require.Equal(t, []string{"sort_order"}, document.Categories[0].Patch.Paths)
	require.Equal(t, []string{"description"}, document.Products[0].Patch.Paths)
	require.NoError(t, document.missingNewFields(
		map[string]string{categoryID: ""},
		map[string]struct{}{productID: {}},
	))

	err = document.missingNewFields(map[string]string{}, map[string]struct{}{})
	require.ElementsMatch(t, []string{
		"categories[0].name: is required",
		"products[0].name: is required",
		"products[0].price_minor: is required",
		"products[0].currency: is required",
		"products[0].availability: is required",
	}, validationTexts(err))
}

func TestParseCatalogFileRejectsIncompletePrice(t *testing.T) {
	_, err := ParseCatalogFile([]byte(`
products:
  - key: item
    id: 20000000-0000-4000-8000-000000000011
    price_minor: 10
`))
	require.Error(t, err)
	require.Contains(t, validationTexts(err), "products[0].currency: is required")
}

func TestMergedCategoryCycleUsesStoredParent(t *testing.T) {
	categories := []SeedCategory{
		{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1", Key: "left", Patch: CategoryPatch{
			ParentID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa2",
			Paths:    []string{"parent_id"},
		}},
		{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa2", Key: "right"},
	}
	err := mergedCategoryCycle(categories, map[string]string{
		categories[0].ID: "",
		categories[1].ID: categories[0].ID,
	})
	require.EqualError(t, err, "categories: must not contain cycles")

	categories[1].Patch.Paths = []string{"parent_id"}
	require.NoError(t, mergedCategoryCycle(categories, map[string]string{
		categories[0].ID: "",
		categories[1].ID: categories[0].ID,
	}))
}

func validationTexts(err error) []string {
	texts := make([]string, 0)
	for _, item := range flattenSeedErrors(err) {
		var validation *apperror.ValidationError
		if errors.As(item, &validation) {
			texts = append(texts, validation.Error())
		}
	}
	return texts
}
