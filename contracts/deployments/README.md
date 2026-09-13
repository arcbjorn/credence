# Deployment manifests

`Deploy.s.sol` writes `<chainid>.json` here. The directory is tracked
because `vm.writeFile` does not create parent directories: without it a
clean checkout fails at deployment, before anything is funded.

Local development manifests (31337, 1337) are ignored — they are
regenerated on every run. A **testnet** manifest is release evidence:
`docs/06` requires the registry address and compatibility evidence in it,
so those are committed deliberately.
