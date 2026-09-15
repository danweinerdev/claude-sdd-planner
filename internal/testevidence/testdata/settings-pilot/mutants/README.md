# Settings pilot defect variants

Each `.go.txt` file is a complete single-file override for the matching implementation file. All other implementation files remain the correct workload version.

| Variant | Override | Isolated contract fault |
|---|---|---|
| `parse-duplicate-last-wins.go.txt` | `parse.go` | Accepts duplicate object keys and retains the last value instead of rejecting duplicates. |
| `render-unsafe-escaping.go.txt` | `render.go` | Inserts keys and values between literal quotes without JSON escaping, so hostile text is not encoded safely. |
| `render-reverse-key-order.go.txt` | `render.go` | Emits keys in descending rather than ascending order. |
| `store-ignores-callback-error.go.txt` | `store.go` | Calls `beforeReplace` but ignores its returned error and replaces the destination. |
| `store-destroys-old-before-callback.go.txt` | `store.go` | Removes an existing destination before invoking `beforeReplace`, so callback failure cannot preserve the prior bytes. |
| `store-load-bypasses-parser.go.txt` | `store.go` | Uses `json.Unmarshal` directly in `Load`, so duplicate keys are silently accepted instead of applying the public parser's rejection behavior. |

`store-load-bypasses-parser.go.txt` is an independent-review-derived holdout/control added after the initial five-variant run. It records an existing contract-coverage gap and makes no performance-advantage claim.
