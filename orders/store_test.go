package orders_test

import (
	"context"
	"errors"
	"testing"

	"bazaar-minggu/db"
	"bazaar-minggu/orders"
)

func TestCreateStore(t *testing.T) {
	ctx := context.Background()
	svc, _ := setup(t)

	res, err := svc.CreateStore(ctx, "whatsapp", "628222@s.whatsapp.net", "628222@s.whatsapp.net", "warteg", "Warteg Bu Tini")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if res.Store.Code != "WARTEG" || res.Role != "organizer" {
		t.Fatalf("result = %+v", res)
	}
	if res.Formatted == "" {
		t.Fatal("formatted reply is empty")
	}

	if _, err := svc.CreateStore(ctx, "whatsapp", "628222@s.whatsapp.net", "628222@s.whatsapp.net", "WARTEG", "Again"); err == nil {
		t.Fatal("want conflict for a duplicate code")
	} else {
		var oe *orders.Error
		if !errors.As(err, &oe) || oe.Code != "conflict" {
			t.Fatalf("err = %v, want conflict", err)
		}
	}

	if _, err := svc.CreateStore(ctx, "", "", "", "X", "Y"); err == nil {
		t.Fatal("want refusal without a sender")
	}
}

func TestGroupClaimFlow(t *testing.T) {
	ctx := context.Background()
	svc, _ := setup(t)

	store, err := svc.CreateStore(ctx, "whatsapp", "628222@s.whatsapp.net", "628222@s.whatsapp.net", "warteg", "Warteg")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	scoped := svc.ForStore(store.Store.ID)

	claim, err := scoped.CreateGroupClaim(ctx, nil, "staff")
	if err != nil {
		t.Fatalf("create claim: %v", err)
	}
	if len(claim.Code) != 6 || claim.ExpiresAt == 0 {
		t.Fatalf("claim = %+v", claim)
	}

	bound, err := svc.RedeemGroupClaim(ctx, claim.Code, "whatsapp", "120363041234567890@g.us", "628333@s.whatsapp.net")
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if bound.Store.ID != store.Store.ID || bound.Role != "staff" {
		t.Fatalf("bound = %+v", bound)
	}

	// The same code is dead, and it fails the same way an unknown one does.
	for _, code := range []string{claim.Code, "ZZZZZZ"} {
		if _, err := svc.RedeemGroupClaim(ctx, code, "whatsapp", "120363049999999999@g.us", "628333@s.whatsapp.net"); err == nil {
			t.Fatalf("code %q redeemed twice", code)
		} else {
			var oe *orders.Error
			if !errors.As(err, &oe) || oe.Code != "claim_invalid" {
				t.Fatalf("err = %v, want claim_invalid", err)
			}
		}
	}

	if _, err := scoped.CreateGroupClaim(ctx, nil, "admin"); err == nil {
		t.Fatal("want invalid_input for an unknown role")
	} else {
		var oe *orders.Error
		if !errors.As(err, &oe) || oe.Code != "invalid_input" {
			t.Fatalf("err = %v, want invalid_input", err)
		}
	}
}

func TestForStoreKeepsReceiver(t *testing.T) {
	ctx := context.Background()
	svc, d := setup(t)

	other := svc.ForStore(99)
	if _, err := other.ListEvents(ctx); err != nil {
		t.Fatalf("list events: %v", err)
	}
	// The receiver still sees its own store.
	events, err := svc.ListEvents(ctx)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events.Events) != 1 {
		t.Fatalf("events = %+v", events.Events)
	}
	if d.StoreID() != 1 {
		t.Fatalf("db store = %d, want 1", d.StoreID())
	}
	var _ = db.RoleStaff
}

func TestStoreAdminIsDirectOnly(t *testing.T) {
	ctx := context.Background()
	svc, _ := setup(t)

	// A group chat is not a person: store creation is refused there.
	if _, err := svc.CreateStore(ctx, "whatsapp", "120363041234567890@g.us", "628222@s.whatsapp.net", "GRP", "From group"); err == nil {
		t.Fatal("want refusal when creating a store from a group")
	} else {
		var oe *orders.Error
		if !errors.As(err, &oe) || oe.Code != "not_allowed" {
			t.Fatalf("err = %v, want not_allowed", err)
		}
	}

	// A binding code must be redeemed in a group, not in a private chat.
	res, err := svc.CreateStore(ctx, "whatsapp", "628222@s.whatsapp.net", "628222@s.whatsapp.net", "DMONLY", "DM only")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	claim, err := svc.ForStore(res.Store.ID).CreateGroupClaim(ctx, nil, "staff")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := svc.RedeemGroupClaim(ctx, claim.Code, "whatsapp", "628222@s.whatsapp.net", "628222@s.whatsapp.net"); err == nil {
		t.Fatal("want refusal when redeeming in a direct message")
	} else {
		var oe *orders.Error
		if !errors.As(err, &oe) || oe.Code != "not_allowed" {
			t.Fatalf("err = %v, want not_allowed", err)
		}
	}
}

func TestCreateStoreRejectsSecondStore(t *testing.T) {
	ctx := context.Background()
	svc, _ := setup(t)

	if _, err := svc.CreateStore(ctx, "whatsapp", "628444@s.whatsapp.net", "628444@s.whatsapp.net", "ONE", "First"); err != nil {
		t.Fatalf("first store: %v", err)
	}
	if _, err := svc.CreateStore(ctx, "whatsapp", "628444@s.whatsapp.net", "628444@s.whatsapp.net", "TWO", "Second"); err == nil {
		t.Fatal("want conflict for a second store")
	} else {
		var oe *orders.Error
		if !errors.As(err, &oe) || oe.Code != "conflict" {
			t.Fatalf("err = %v, want conflict", err)
		}
	}
}
