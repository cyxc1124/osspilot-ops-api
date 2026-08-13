package grants

func uniqueIDs(ids []int64) ([]int64, bool) {
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id < 1 {
			return nil, false
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, true
}

func exceedsLimit(n int, limit *int64) bool {
	return limit != nil && int64(n) > *limit
}
