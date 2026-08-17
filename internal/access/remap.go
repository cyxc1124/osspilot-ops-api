package access

import "github.com/cyxc1124/osspilot-ops-api/internal/project"

func bindOpsIDs(items []project.AccessItem, byName map[string]int64) []project.AccessItem {
	out := make([]project.AccessItem, 0, len(items))
	for _, it := range items {
		id, ok := byName[it.AccountName]
		if !ok {
			continue
		}
		it.AccountID = id
		out = append(out, it)
	}
	return out
}
