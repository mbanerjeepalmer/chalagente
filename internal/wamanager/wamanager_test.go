package wamanager

import (
	"sync"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func TestRegistryLookupsEmpty(t *testing.T) {
	m := New(nil, nil)

	if _, ok := m.Client("biz1"); ok {
		t.Fatal("Client should be missing")
	}
	if _, ok := m.DeviceJID("biz1"); ok {
		t.Fatal("DeviceJID should be missing")
	}
	if _, ok := m.BusinessForDeviceJID(types.NewJID("12345", types.DefaultUserServer)); ok {
		t.Fatal("BusinessForDeviceJID should be missing")
	}
}

func TestRegistryTrackUntrack(t *testing.T) {
	m := New(nil, nil)
	jid := types.NewJID("447700900123", types.DefaultUserServer)

	e := &entry{BusinessID: "biz1", DeviceJID: jid}
	m.track(e)

	gotBiz, ok := m.BusinessForDeviceJID(jid)
	if !ok || gotBiz != "biz1" {
		t.Fatalf("BusinessForDeviceJID: got (%q, %v); want (biz1, true)", gotBiz, ok)
	}
	gotJID, ok := m.DeviceJID("biz1")
	if !ok || gotJID.String() != jid.String() {
		t.Fatalf("DeviceJID: got (%s, %v); want (%s, true)", gotJID, ok, jid)
	}

	m.untrack("biz1")
	if _, ok := m.BusinessForDeviceJID(jid); ok {
		t.Fatal("after untrack, BusinessForDeviceJID should miss")
	}
	if _, ok := m.DeviceJID("biz1"); ok {
		t.Fatal("after untrack, DeviceJID should miss")
	}
}

func TestRegistryTrackWithoutJID(t *testing.T) {
	m := New(nil, nil)
	m.track(&entry{BusinessID: "biz1"})

	if _, ok := m.DeviceJID("biz1"); ok {
		t.Fatal("DeviceJID should miss when JID empty")
	}
	if _, ok := m.BusinessForDeviceJID(types.JID{}); ok {
		t.Fatal("empty JID lookup should miss")
	}
}

func TestDispatchCallsHandlerWithBusinessID(t *testing.T) {
	m := New(nil, nil)
	var got struct {
		sync.Mutex
		biz string
		evt any
	}
	m.SetEventHandler(func(businessID string, evt any) {
		got.Lock()
		defer got.Unlock()
		got.biz = businessID
		got.evt = evt
	})
	m.dispatch("biz1")("hello")

	got.Lock()
	defer got.Unlock()
	if got.biz != "biz1" {
		t.Fatalf("biz: got %q want biz1", got.biz)
	}
	if got.evt != "hello" {
		t.Fatalf("evt: got %v want hello", got.evt)
	}
}

func TestDispatchNoHandler(t *testing.T) {
	m := New(nil, nil)
	m.dispatch("biz1")("hello") // should not panic
}

func TestLogoutUnregistered(t *testing.T) {
	m := New(nil, nil)
	if err := m.Logout(t.Context(), "missing"); err != ErrNotRegistered {
		t.Fatalf("Logout missing: got %v, want ErrNotRegistered", err)
	}
}

func TestTenantsSnapshot(t *testing.T) {
	m := New(nil, nil)
	if got := m.Tenants(); len(got) != 0 {
		t.Fatalf("empty manager: got %d tenants, want 0", len(got))
	}
}
