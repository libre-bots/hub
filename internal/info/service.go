package info

import (
	"context"

	hubv1 "github.com/libre-bots/hub/gen/librebots/hub/v1"
)

type Service struct {
	hubv1.UnimplementedInfoServiceServer

	version string
}

// NewService reports version, or "dev" when it is empty.
func NewService(version string) *Service {
	if version == "" {
		version = "dev"
	}
	return &Service{version: version}
}

func (s *Service) GetInfo(context.Context, *hubv1.GetInfoRequest) (*hubv1.GetInfoResponse, error) {
	return &hubv1.GetInfoResponse{Version: s.version}, nil
}
