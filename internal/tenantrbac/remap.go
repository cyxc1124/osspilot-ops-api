package tenantrbac

import "encoding/json"

// remapAccountID renames tenant_id → account_id and sets the ops account id.
// User/bucket ids stay as tenant-side values.
func remapAccountID(raw []byte, opsAccountID int64) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw, nil // ponytail: non-JSON success bodies pass through.
	}
	walkRemap(v, opsAccountID)
	out, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func walkRemap(v any, opsAccountID int64) {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["tenant_id"]; ok {
			delete(x, "tenant_id")
			x["account_id"] = opsAccountID
		}
		for _, child := range x {
			walkRemap(child, opsAccountID)
		}
	case []any:
		for _, child := range x {
			walkRemap(child, opsAccountID)
		}
	}
}
