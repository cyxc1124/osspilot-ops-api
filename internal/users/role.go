package users

import (
	"fmt"
	"sort"
	"strings"
)

const (
	roleAdmin    = "platform_admin"
	roleOperator = "ops_operator"
)

func normalizeRole(names []string) (string, error) {
	invalid := []string{}
	seen := map[string]struct{}{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if n != roleAdmin && n != roleOperator {
			invalid = append(invalid, n)
			continue
		}
		seen[n] = struct{}{}
	}
	if len(invalid) > 0 {
		sort.Strings(invalid)
		return "", fmt.Errorf("Invalid ops role(s): %s", strings.Join(invalid, ", "))
	}
	// ponytail: one role column until O7; platform_admin wins if both sent
	if _, ok := seen[roleAdmin]; ok {
		return roleAdmin, nil
	}
	if _, ok := seen[roleOperator]; ok {
		return roleOperator, nil
	}
	return "", nil
}

func rolesJSON(role string) []string {
	if role == "" {
		return []string{}
	}
	return []string{role}
}
