package grpcapi

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	marketv1 "github.com/SV1Stail/online_market/gen/online_market/v1"
	"github.com/SV1Stail/online_market/internal/auth"
)

type AdminAuthServer struct {
	marketv1.UnimplementedAdminAuthServiceServer
	service *auth.Service
}

func NewAdminAuthServer(service *auth.Service) *AdminAuthServer {
	return &AdminAuthServer{service: service}
}

func (s *AdminAuthServer) Login(ctx context.Context, request *marketv1.LoginRequest) (*marketv1.LoginResponse, error) {
	result, err := s.service.Login(ctx, request.GetLogin(), request.GetPassword())
	if err != nil {
		return nil, err
	}
	return &marketv1.LoginResponse{
		User:                  authUserToProto(result.User),
		AccessToken:           result.AccessToken,
		RefreshToken:          result.RefreshToken,
		AccessTokenExpiresAt:  timestamppb.New(result.AccessTokenExpiresAt),
		RefreshTokenExpiresAt: timestamppb.New(result.RefreshTokenExpiresAt),
	}, nil
}

func (s *AdminAuthServer) RefreshSession(ctx context.Context, request *marketv1.RefreshSessionRequest) (*marketv1.RefreshSessionResponse, error) {
	result, err := s.service.Refresh(ctx, request.GetRefreshToken())
	if err != nil {
		return nil, err
	}
	return &marketv1.RefreshSessionResponse{
		AccessToken:           result.AccessToken,
		RefreshToken:          result.RefreshToken,
		AccessTokenExpiresAt:  timestamppb.New(result.AccessTokenExpiresAt),
		RefreshTokenExpiresAt: timestamppb.New(result.RefreshTokenExpiresAt),
	}, nil
}

func (s *AdminAuthServer) Logout(ctx context.Context, request *marketv1.LogoutRequest) (*marketv1.LogoutResponse, error) {
	if err := s.service.Logout(ctx, request.GetRefreshToken()); err != nil {
		return nil, err
	}
	return &marketv1.LogoutResponse{}, nil
}
