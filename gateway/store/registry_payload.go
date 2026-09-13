package store

import (
	"encoding/json"
	"fmt"
)

// The watch payload is stored as JSON so a schema migration is not needed
// to carry an extra field, and so SQLite's json_extract can join on it.

func loanWatchKey(bindingKey string) string {
	return "watch:" + bindingKey
}

func watchPayload(bindingKey, loanID string, stateVersion uint64, observedAt int64) []byte {
	raw, _ := json.Marshal(map[string]any{
		"binding_key":           bindingKey,
		"loan_id":               loanID,
		"account_state_version": stateVersion,
		"observed_at":           observedAt,
	})
	return raw
}

func parseWatchPayload(raw []byte) (ActiveWatch, error) {
	var doc struct {
		BindingKey   string `json:"binding_key"`
		LoanID       string `json:"loan_id"`
		StateVersion uint64 `json:"account_state_version"`
		ObservedAt   int64  `json:"observed_at"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ActiveWatch{}, fmt.Errorf("parse watch payload: %w", err)
	}
	if doc.BindingKey == "" || doc.LoanID == "" {
		return ActiveWatch{}, fmt.Errorf("watch payload missing binding or loan")
	}
	return ActiveWatch{
		BindingKey:          doc.BindingKey,
		LoanID:              doc.LoanID,
		AccountStateVersion: doc.StateVersion,
		PermissionIssuedAt:  doc.ObservedAt,
		EvidenceObservedAt:  doc.ObservedAt,
	}, nil
}
