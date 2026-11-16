package catalog

import (
	"context"
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

func (s *adminRepositoryStub) UpdateProduct(context.Context, string, uint64, ProductPatch) (*Product, error) {
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

type adminIDGeneratorStub struct {
	id string
}

func (s adminIDGeneratorStub) UUID() (string, error) {
	return s.id, nil
}
