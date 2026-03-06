module github.com/openbotstack/openbotstack-runtime

go 1.26.1

replace github.com/openbotstack/openbotstack-core => ../openbotstack-core

require (
	github.com/google/uuid v1.6.0
	github.com/openbotstack/openbotstack-core v0.0.0-00010101000000-000000000000
	github.com/tetratelabs/wazero v1.11.0
	gopkg.in/yaml.v3 v3.0.1
)

require golang.org/x/sys v0.38.0 // indirect
