# Refuse publicly-known keys on a public chain.
#
# Anvil prints its ten private keys in its startup banner, so they are
# published, not merely weak. Using one locally is correct and convenient;
# using one on a chain anybody can reach means anybody can sign what that
# key signs -- for the attestor, that is the authority the oracle trusts
# for the whole route.
#
# Sourced by the demo scripts before anything is deployed or signed.
# Reported by review, 14 September: the testnet attestor was anvil #3.

# The keys anvil prints. Listed in full rather than by index so a
# comparison cannot silently pass on a key this file does not know.
CREDENCE_PUBLIC_KEYS="
ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80
59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d
5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a
7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6
47e179ec197488593b187f80a00eb0da91f1b9d0b13f8733639f19c30a34926a
8b3a350cf5c34c9194ca85829a2df0ec3153be0318b5e2d3348e872092edffba
92db14e403b83dfe3df233f83dfa3a0d7096f21ca9b0d6d6b8d88b2b4ec1564e
4bbbf85ce3377467afe5d46f804f221813b2bb87f24d81f60f1fcdbf7cbf4356
dbda1821b80551c9d65939329250298aa3472ba22feea921c0cf5d620ea67b97
2a871d0798f97d79848a013d4936a73bf4cc922c825d33c1cf7073dff6d409c6
"

# A chain id that only exists on a developer's own machine.
credence_is_local_chain() {
  case "$1" in
    31337|1337) return 0 ;;
    *) return 1 ;;
  esac
}

# credence_require_private_key <key-hex> <chain-id> <role>
#
# Exits non-zero when a published key would be used off a local chain.
# Failing here is the point: a run that proceeds and signs is a run whose
# attestations anyone can forge.
credence_require_private_key() {
  local key="${1#0x}" chain="$2" role="$3"

  if credence_is_local_chain "$chain"; then
    return 0
  fi

  local known
  for known in $CREDENCE_PUBLIC_KEYS; do
    if [ "$key" = "$known" ]; then
      cat >&2 <<MSG

refusing to use a published key as the $role on chain $chain.

This is one of the private keys anvil prints in its startup banner, so
anyone can sign with it. On a local chain that is harmless; here it would
mean the $role's authority is available to anybody who reads the banner.

Generate one and export it:

  cast wallet new
  export ${4:-CREDENCE_SIGNER_KEY}=<the private key, no 0x>

MSG
      return 1
    fi
  done
  return 0
}
