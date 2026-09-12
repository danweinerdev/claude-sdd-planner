package hook

import (
	jsonpkg "encoding/json"
)

func jsonUnmarshal(raw []byte, out any) error { return jsonpkg.Unmarshal(raw, out) }
