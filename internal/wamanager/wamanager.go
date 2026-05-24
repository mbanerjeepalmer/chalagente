package wamanager

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var (
	ErrNotRegistered = errors.New("wamanager: business not registered")
	ErrNotLoggedIn   = errors.New("wamanager: client not logged in")
)

type EventHandler func(businessID string, evt any)

type entry struct {
	BusinessID string
	DeviceJID  types.JID
	Client     *whatsmeow.Client
}

type Manager struct {
	container *sqlstore.Container
	logger    waLog.Logger
	onEvent   EventHandler

	mu    sync.RWMutex
	byBiz map[string]*entry
	byJID map[string]*entry
}

func New(container *sqlstore.Container, logger waLog.Logger) *Manager {
	if logger == nil {
		logger = waLog.Noop
	}
	return &Manager{
		container: container,
		logger:    logger,
		byBiz:     make(map[string]*entry),
		byJID:     make(map[string]*entry),
	}
}

func (m *Manager) SetEventHandler(fn EventHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEvent = fn
}

func (m *Manager) track(e *entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byBiz[e.BusinessID] = e
	if !e.DeviceJID.IsEmpty() {
		m.byJID[e.DeviceJID.String()] = e
	}
}

func (m *Manager) untrack(businessID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.byBiz[businessID]
	if !ok {
		return
	}
	delete(m.byBiz, businessID)
	if !e.DeviceJID.IsEmpty() {
		delete(m.byJID, e.DeviceJID.String())
	}
}

func (m *Manager) Client(businessID string) (*whatsmeow.Client, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.byBiz[businessID]
	if !ok {
		return nil, false
	}
	return e.Client, true
}

func (m *Manager) DeviceJID(businessID string) (types.JID, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.byBiz[businessID]
	if !ok || e.DeviceJID.IsEmpty() {
		return types.JID{}, false
	}
	return e.DeviceJID, true
}

func (m *Manager) BusinessForDeviceJID(jid types.JID) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.byJID[jid.String()]
	if !ok {
		return "", false
	}
	return e.BusinessID, true
}

func (m *Manager) dispatch(businessID string) func(evt any) {
	return func(evt any) {
		m.mu.RLock()
		fn := m.onEvent
		m.mu.RUnlock()
		if fn != nil {
			fn(businessID, evt)
		}
	}
}

func (m *Manager) StartPairing(ctx context.Context, businessID string) (<-chan whatsmeow.QRChannelItem, error) {
	device := m.container.NewDevice()
	client := whatsmeow.NewClient(device, m.logger)
	client.AddEventHandler(m.dispatch(businessID))

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return nil, fmt.Errorf("get qr channel: %w", err)
	}
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	e := &entry{BusinessID: businessID, Client: client}
	m.track(e)

	wrapped := make(chan whatsmeow.QRChannelItem, 4)
	go func() {
		defer close(wrapped)
		for item := range qrChan {
			if item.Event == "success" {
				if client.Store.ID != nil {
					m.mu.Lock()
					e.DeviceJID = *client.Store.ID
					m.byJID[e.DeviceJID.String()] = e
					m.mu.Unlock()
				}
			}
			wrapped <- item
		}
	}()
	return wrapped, nil
}

func (m *Manager) StartPaired(ctx context.Context, businessID string, deviceJID types.JID) error {
	device, err := m.container.GetDevice(ctx, deviceJID)
	if err != nil {
		return fmt.Errorf("get device %s: %w", deviceJID, err)
	}
	if device == nil {
		return fmt.Errorf("device %s not in store", deviceJID)
	}
	client := whatsmeow.NewClient(device, m.logger)
	client.AddEventHandler(m.dispatch(businessID))
	if err := client.Connect(); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	m.track(&entry{BusinessID: businessID, DeviceJID: deviceJID, Client: client})
	return nil
}

func (m *Manager) Disconnect(businessID string) {
	m.mu.RLock()
	e, ok := m.byBiz[businessID]
	m.mu.RUnlock()
	if !ok {
		return
	}
	e.Client.Disconnect()
	m.untrack(businessID)
}

func (m *Manager) Shutdown() {
	m.mu.RLock()
	entries := make([]*entry, 0, len(m.byBiz))
	for _, e := range m.byBiz {
		entries = append(entries, e)
	}
	m.mu.RUnlock()
	for _, e := range entries {
		e.Client.Disconnect()
	}
}

func (m *Manager) Tenants() []TenantStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]TenantStatus, 0, len(m.byBiz))
	for _, e := range m.byBiz {
		out = append(out, TenantStatus{
			BusinessID: e.BusinessID,
			DeviceJID:  e.DeviceJID,
			Connected:  e.Client.IsConnected(),
			LoggedIn:   e.Client.IsLoggedIn(),
		})
	}
	return out
}

type TenantStatus struct {
	BusinessID string
	DeviceJID  types.JID
	Connected  bool
	LoggedIn   bool
}
