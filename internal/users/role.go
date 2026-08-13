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

func normalizeRoles(names []string) ([]string, error) {
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
		return nil, fmt.Errorf("Invalid ops role(s): %s", strings.Join(invalid, ", "))
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}
