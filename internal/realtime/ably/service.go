package ably

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	ablysdk "github.com/ably/ably-go/ably"

	"watchtogether/internal/config"
	"watchtogether/internal/room"
)

const (
	MessageNameSnapshot = "room.snapshot"
	MessageNameSync     = "room.sync"
	MessageNameEvent    = "room.event"
	MessageNameError    = "room.error"
	MessageNameControl  = "room.control"
)

var ErrRootKeyRequired = errors.New("ably root key is required")

type TokenDetails = ablysdk.TokenDetails

type Publisher interface {
	ChannelName(roomID string) string
	RoomCapability(roomID string) (string, error)
	RequestRoomToken(ctx context.Context, roomID, clientID string) (*ablysdk.TokenDetails, error)
	PublishRoomMessage(ctx context.Context, roomID, name string, data room.Message) error
}

type Service struct {
	rest *ablysdk.REST
	cfg  config.Config
}

func NewService(cfg config.Config) (*Service, error) {
	if strings.TrimSpace(cfg.AblyRootKey) == "" {
		return nil, ErrRootKeyRequired
	}
	rest, err := ablysdk.NewREST(ablysdk.WithKey(cfg.AblyRootKey))
	if err != nil {
		return nil, err
	}
	return &Service{rest: rest, cfg: cfg}, nil
}

func (s *Service) ChannelName(roomID string) string {
	return ChannelName(s.cfg.AblyChannelPrefix, roomID)
}

func ChannelName(prefix, roomID string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), ":")
	roomID = strings.TrimSpace(roomID)
	return fmt.Sprintf("%s:room:%s:control", prefix, roomID)
}

func AdminChannelName(prefix string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), ":")
	return prefix + ":admin"
}

func (s *Service) RoomCapability(roomID string) (string, error) {
	return RoomCapability(s.ChannelName(roomID))
}

func RoomCapability(channel string) (string, error) {
	raw, err := json.Marshal(map[string][]string{
		channel: {"subscribe", "presence", "history"},
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *Service) RequestRoomToken(ctx context.Context, roomID, clientID string) (*ablysdk.TokenDetails, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, errors.New("client id is required")
	}
	capability, err := s.RoomCapability(roomID)
	if err != nil {
		return nil, err
	}
	return s.rest.Auth.RequestToken(ctx, &ablysdk.TokenParams{
		ClientID:   clientID,
		TTL:        s.cfg.AblyTokenTTL.Milliseconds(),
		Capability: capability,
	})
}

func (s *Service) PublishRoomMessage(ctx context.Context, roomID, name string, data any) error {
	return s.rest.Channels.Get(s.ChannelName(roomID)).Publish(ctx, name, data)
}
