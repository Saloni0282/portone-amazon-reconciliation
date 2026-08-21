package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var payDateRegex = regexp.MustCompile(`(?i)^(\d+)\s+([A-Za-z]+)\s+(\d{4})\s+(\d+):(\d+):(\d+)\s+(am|pm)\s+GMT([+-]?\d+)?$`)

var monthMap = map[string]time.Month{
	"january": time.January, "february": time.February, "march": time.March, "april": time.April,
	"may": time.May, "june": time.June, "july": time.July, "august": time.August,
	"september": time.September, "october": time.October, "november": time.November, "december": time.December,
	"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
}

// Normalize uppercase and replaces spaces with underscores
func Normalize(val string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(val), " ", "_"))
}

// ParsePaymentsDate parses "13 July 2026 2:25:31 am GMT+9" or "20 July 2026 5:54:19 pm GMT+9" to UTC time.
func ParsePaymentsDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(dateStr)
	m := payDateRegex.FindStringSubmatch(dateStr)
	if m == nil {
		return time.Time{}, fmt.Errorf("invalid payments date format: %s", dateStr)
	}

	day, _ := strconv.Atoi(m[1])
	monthStr := strings.ToLower(m[2])
	year, _ := strconv.Atoi(m[3])
	hour, _ := strconv.Atoi(m[4])
	minute, _ := strconv.Atoi(m[5])
	second, _ := strconv.Atoi(m[6])
	ampm := strings.ToLower(m[7])
	tzOffsetStr := m[8]

	month, ok := monthMap[monthStr]
	if !ok {
		return time.Time{}, fmt.Errorf("unknown month name: %s", monthStr)
	}

	if ampm == "pm" && hour < 12 {
		hour += 12
	} else if ampm == "am" && hour == 12 {
		hour = 0
	}

	offsetHours := 0
	if tzOffsetStr != "" {
		offsetHours, _ = strconv.Atoi(tzOffsetStr)
	}

	// Create location for the timezone offset
	loc := time.FixedZone(fmt.Sprintf("GMT%+d", offsetHours), offsetHours*60*60)
	localTime := time.Date(year, month, day, hour, minute, second, 0, loc)

	return localTime.UTC(), nil
}

// ParseSettlementsDate parses "20.07.2026 08:54:19 UTC" to UTC time.
func ParseSettlementsDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(strings.ReplaceAll(dateStr, "UTC", ""))
	t, err := time.Parse("02.01.2006 15:04:05", dateStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid settlements date format: %s | %w", dateStr, err)
	}
	return t.UTC(), nil
}

// MatchRulePayments checks if a payments row matches a config rule.
func MatchRulePayments(rowType, rowDesc string, cfgType, cfgDesc string) bool {
	// 1. Check transaction type (case-insensitive and normalized)
	if cfgType != "" && cfgType != rowType {
		return false
	}

	// 2. Check description matching
	// Wildcard match (either "ANY" or "*")
	if cfgDesc == "ANY" || cfgDesc == "*" {
		return true
	}

	// Exact match
	if cfgDesc == rowDesc {
		return true
	}

	// Prefix matching where Semantics dictate (exclusively for TRANSFER TO_ACCOUNT_ENDING/TO_YOUR_ACCOUNT_ENDING rules)
	if cfgType == "TRANSFER" && (strings.HasPrefix(cfgDesc, "TO_ACCOUNT_ENDING") || strings.HasPrefix(cfgDesc, "TO_YOUR_ACCOUNT_ENDING")) {
		if strings.HasPrefix(rowDesc, cfgDesc) {
			return true
		}
	}

	return false
}

// MatchRuleSettlements checks if a settlements row matches a config rule.
func MatchRuleSettlements(rowType, rowAmtType, rowAmtDesc string, cfgType, cfgAmtType, cfgAmtDesc string) bool {
	// 1. Check transaction type (case-insensitive and normalized)
	if cfgType != "" && cfgType != rowType {
		return false
	}

	// 2. Check amount type matching
	if cfgAmtType != "ANY" && cfgAmtType != "*" && cfgAmtType != rowAmtType {
		return false
	}

	// 3. Check description matching
	// Wildcard match (either "ANY" or "*")
	if cfgAmtDesc == "ANY" || cfgAmtDesc == "*" {
		return true
	}

	// Exact match
	if cfgAmtDesc == rowAmtDesc {
		return true
	}

	return false
}
