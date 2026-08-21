package main

import (
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Order", "ORDER"},
		{"FBA Fees", "FBA_FEES"},
		{"  some value  ", "SOME_VALUE"},
		{"already_normalized", "ALREADY_NORMALIZED"},
	}

	for _, tc := range tests {
		got := Normalize(tc.input)
		if got != tc.expected {
			t.Errorf("Normalize(%q) = %q; expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestParsePaymentsDate(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Time
		hasError bool
	}{
		{
			"13 July 2026 2:25:31 am GMT+9",
			time.Date(2026, 7, 12, 17, 25, 31, 0, time.UTC),
			false,
		},
		{
			"20 July 2026 5:54:19 pm GMT+10",
			time.Date(2026, 7, 20, 7, 54, 19, 0, time.UTC),
			false,
		},
		{
			"15 July 2026 12:00:00 am GMT",
			time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
			false,
		},
		{
			"invalid date string",
			time.Time{},
			true,
		},
	}

	for _, tc := range tests {
		got, err := ParsePaymentsDate(tc.input)
		if tc.hasError {
			if err == nil {
				t.Errorf("ParsePaymentsDate(%q) expected error; got none", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParsePaymentsDate(%q) unexpected error: %v", tc.input, err)
			} else if !got.Equal(tc.expected) {
				t.Errorf("ParsePaymentsDate(%q) = %v; expected %v", tc.input, got, tc.expected)
			}
		}
	}
}

func TestParseSettlementsDate(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Time
		hasError bool
	}{
		{
			"20.07.2026 08:54:19 UTC",
			time.Date(2026, 7, 20, 8, 54, 19, 0, time.UTC),
			false,
		},
		{
			"25.12.2026 23:59:59",
			time.Date(2026, 12, 25, 23, 59, 59, 0, time.UTC),
			false,
		},
		{
			"invalid",
			time.Time{},
			true,
		},
	}

	for _, tc := range tests {
		got, err := ParseSettlementsDate(tc.input)
		if tc.hasError {
			if err == nil {
				t.Errorf("ParseSettlementsDate(%q) expected error; got none", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseSettlementsDate(%q) unexpected error: %v", tc.input, err)
			} else if !got.Equal(tc.expected) {
				t.Errorf("ParseSettlementsDate(%q) = %v; expected %v", tc.input, got, tc.expected)
			}
		}
	}
}

func TestMatchRulePayments(t *testing.T) {
	tests := []struct {
		rowType  string
		rowDesc  string
		cfgType  string
		cfgDesc  string
		expected bool
	}{
		// Exact matches
		{"ORDER", "PRODUCT_CHARGES", "ORDER", "PRODUCT_CHARGES", true},
		{"ORDER", "PRODUCT_CHARGES", "REFUND", "PRODUCT_CHARGES", false},

		// Wildcard matches
		{"ORDER", "ANYTHING", "ORDER", "ANY", true},
		{"ORDER", "ANYTHING", "ORDER", "*", true},

		// Fallback / Blank types in configs
		{"ORDER", "ANYTHING", "", "*", true},

		// Prefix matching allowed only for TRANSFER TO_ACCOUNT_ENDING / TO_YOUR_ACCOUNT_ENDING
		{"TRANSFER", "TO_ACCOUNT_ENDING_WITH:_334", "TRANSFER", "TO_ACCOUNT_ENDING", true},
		{"TRANSFER", "TO_ACCOUNT_ENDING_WITH:_334", "TRANSFER", "TO_ACCOUNT_ENDING_WITH:_334", true},
		{"TRANSFER", "TO_ACCOUNT_ENDING_WITH:_334", "TRANSFER", "OTHER_PREFIX", false},
		
		// Unrelated prefix must NOT match
		{"ORDER", "PRODUCT_CHARGES_SUFFIX", "ORDER", "PRODUCT_CHARGES", false},
		{"SERVICE_FEE", "FBA_STORAGE_FEE_EXTRA", "SERVICE_FEE", "FBA_STORAGE_FEE", false},
	}

	for _, tc := range tests {
		got := MatchRulePayments(tc.rowType, tc.rowDesc, tc.cfgType, tc.cfgDesc)
		if got != tc.expected {
			t.Errorf("MatchRulePayments(%q, %q, %q, %q) = %v; expected %v",
				tc.rowType, tc.rowDesc, tc.cfgType, tc.cfgDesc, got, tc.expected)
		}
	}
}

func TestMatchRuleSettlements(t *testing.T) {
	tests := []struct {
		rowType    string
		rowAmtType string
		rowAmtDesc string
		cfgType    string
		cfgAmtType string
		cfgAmtDesc string
		expected   bool
	}{
		// Exact Match
		{"ORDER", "PRODUCT_CHARGES", "PRINCIPAL", "ORDER", "PRODUCT_CHARGES", "PRINCIPAL", true},
		{"ORDER", "PRODUCT_CHARGES", "PRINCIPAL", "ORDER", "PRODUCT_CHARGES", "OTHER", false},

		// Wildcard Match
		{"ORDER", "PRODUCT_CHARGES", "PRINCIPAL", "ORDER", "PRODUCT_CHARGES", "ANY", true},
		{"ORDER", "PRODUCT_CHARGES", "PRINCIPAL", "ORDER", "PRODUCT_CHARGES", "*", true},

		// Fallback Match (empty transaction type)
		{"OTHER_TXN", "PRODUCT_CHARGES", "PRINCIPAL", "", "PRODUCT_CHARGES", "PRINCIPAL", true},
		{"OTHER_TXN", "PRODUCT_CHARGES", "PRINCIPAL", "", "ANY", "*", true},
	}

	for _, tc := range tests {
		got := MatchRuleSettlements(tc.rowType, tc.rowAmtType, tc.rowAmtDesc, tc.cfgType, tc.cfgAmtType, tc.cfgAmtDesc)
		if got != tc.expected {
			t.Errorf("MatchRuleSettlements(%q, %q, %q, %q, %q, %q) = %v; expected %v",
				tc.rowType, tc.rowAmtType, tc.rowAmtDesc, tc.cfgType, tc.cfgAmtType, tc.cfgAmtDesc, got, tc.expected)
		}
	}
}
