package order

import (
	"errors"
	"testing"
	"time"

	"github.com/five07/gt-order-fsm/internal/payment"
	"golang.org/x/text/currency"
)

func TestNew(t *testing.T) {
	at := time.Date(2026, 10, 27, 9, 0, 0, 0, time.FixedZone("MDT", -6*60*60))
	o := New("ord_1", payment.Money{Amount: 100, Currency: currency.USD}, "4242", at)

	if o.State != StateInitialized {
		t.Errorf("State = %s, want %s", o.State, StateInitialized)
	}
	if o.CreatedAt.Location() != time.UTC || !o.CreatedAt.Equal(at) {
		t.Errorf("CreatedAt = %v, want %v in UTC", o.CreatedAt, at)
	}
	want := HistoryEntry{To: StateInitialized, At: at.UTC()}
	if len(o.History) != 1 || o.History[0] != want {
		t.Errorf("History = %+v, want [%+v]", o.History, want)
	}
}

func TestApplyInvalidLeavesOrderUnchanged(t *testing.T) {
	at := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	o := New("ord_1", payment.Money{Amount: 100, Currency: currency.USD}, "4242", at)

	err := o.apply(EventFulfill, at.Add(time.Second), "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("apply error = %v, want ErrInvalidTransition", err)
	}
	if o.State != StateInitialized || len(o.History) != 1 || !o.UpdatedAt.Equal(at) {
		t.Errorf("order changed: state %s, %d entries, updated %v", o.State, len(o.History), o.UpdatedAt)
	}
}

func TestApplyRecordsJoinedErrors(t *testing.T) {
	at := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	o := New("ord_1", payment.Money{Amount: 100, Currency: currency.USD}, "4242", at)
	o.State = StatePaymentAuthorized
	errA, errB := errors.New("a"), errors.New("b")

	if err := o.apply(EventEscalate, at.Add(time.Second), "note", errA, errB); err != nil {
		t.Fatalf("apply: %v", err)
	}
	last := o.History[len(o.History)-1]
	if !errors.Is(last.Err, errA) || !errors.Is(last.Err, errB) {
		t.Errorf("Err = %v, want it to match both causes", last.Err)
	}
	if last.Note != "note" {
		t.Errorf("Note = %q, want %q", last.Note, "note")
	}
}

func TestCloneIsIndependent(t *testing.T) {
	at := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	o := New("ord_1", payment.Money{Amount: 100, Currency: currency.USD}, "4242", at)
	o.Authorization = &payment.Authorization{ID: "auth_1", Status: payment.Success}

	c := o.Clone()
	c.History[0].To = StateRejected
	c.History = append(c.History, HistoryEntry{To: StateComplete})
	c.Authorization.ID = "auth_2"

	if o.History[0].To != StateInitialized || len(o.History) != 1 {
		t.Errorf("original history changed: %+v", o.History)
	}
	if o.Authorization.ID != "auth_1" {
		t.Errorf("original authorization changed: %+v", o.Authorization)
	}
}

func TestCloneWithoutAuthorization(t *testing.T) {
	o := New("ord_1", payment.Money{Amount: 100, Currency: currency.USD}, "4242", time.Now())
	if c := o.Clone(); c.Authorization != nil {
		t.Errorf("Authorization = %+v, want nil", c.Authorization)
	}
}
