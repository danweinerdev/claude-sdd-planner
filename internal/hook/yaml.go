package hook

import (
	jsonpkg "encoding/json"

	"gopkg.in/yaml.v3"
)

func yamlUnmarshal(src string, out any) error { return yaml.Unmarshal([]byte(src), out) }

func jsonUnmarshal(raw []byte, out any) error { return jsonpkg.Unmarshal(raw, out) }
