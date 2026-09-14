#!/usr/bin/env bash
# Resolve and verify the external dependencies on whatever chain RPC points at.
#
# `docs/06` requires the registry address AND compatibility evidence in the
# deployment manifest. Documentation is not evidence: this reads deployed
# bytecode, so a moved or absent contract fails loudly rather than at demo
# time.
#
# ERC-8004 publishes two canonical address pairs -- one shared by every
# mainnet, one shared by every testnet. Rather than hardcode a pair and
# explain the other in a comment, both are tried and the chain decides.
# A comment cannot stop someone using the wrong one; discovery can.
set -euo pipefail

RPC=${RPC:-https://testnet-rpc.monad.xyz}

# Canonical ERC-8004 deployments, same addresses on every chain of a tier.
MAINNET_IDENTITY=0x8004A169FB4a3325136EB29fA0ceB6D2e539a432
MAINNET_REPUTATION=0x8004BAa17C55a88189AE136b182e5fdA19dE9b63
TESTNET_IDENTITY=0x8004A818BFB912233c491871b3d84c89A494BD9e
TESTNET_REPUTATION=0x8004B663056A597Dffe9eCcC1965A193B7388713

# Chainlink CRE, per network. Override for a chain other than Monad.
CRE_FORWARDER=${CRE_FORWARDER:-0xF8344CFd5c43616a4366C34E3EEE75af79a74482}

# EIP-1967 implementation slot: the registries are proxies, so
# compatibility must be checked against the implementation behind them.
IMPL_SLOT=0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc

fail=0
CHAIN_ID=$(cast chain-id --rpc-url "$RPC")
echo "chain id: $CHAIN_ID"
echo

# Deployed code, or nothing. Compared as a string rather than by byte
# count: `wc -c` on a piped value counts the trailing newline, so an empty
# "0x" measures 3 and a `-gt 2` test reports every absent contract as
# present -- which made this whole probe pass against a bare chain.
has_code() {
  local code
  code=$(cast code "$1" --rpc-url "$RPC" 2>/dev/null)
  [ -n "$code" ] && [ "$code" != "0x" ]
}

# Deployed size in bytes, for reporting.
code_size() {
  local code
  code=$(cast code "$1" --rpc-url "$RPC" 2>/dev/null)
  echo $(( (${#code} - 2) / 2 ))
}

# --- Resolve the registry tier by asking the chain ---

if has_code "$TESTNET_IDENTITY"; then
  TIER=testnet
  IDENTITY=$TESTNET_IDENTITY
  REPUTATION=$TESTNET_REPUTATION
elif has_code "$MAINNET_IDENTITY"; then
  TIER=mainnet
  IDENTITY=$MAINNET_IDENTITY
  REPUTATION=$MAINNET_REPUTATION
else
  echo "ERC-8004: neither the mainnet nor the testnet registry is deployed here."
  echo "  tried mainnet $MAINNET_IDENTITY"
  echo "  tried testnet $TESTNET_IDENTITY"
  echo "This chain has no canonical ERC-8004 deployment."
  exit 1
fi

echo "ERC-8004 resolved to the $TIER deployment:"
echo "  identity   $IDENTITY"
echo "  reputation $REPUTATION"

if ! has_code "$REPUTATION"; then
  echo "  MISSING reputation registry at the $TIER address"
  fail=1
fi
echo

# --- Identity registry is the agent registry it claims to be ---

echo "identity registry:"
echo "  name   $(cast call "$IDENTITY" 'name()(string)' --rpc-url "$RPC")"
echo "  symbol $(cast call "$IDENTITY" 'symbol()(string)' --rpc-url "$RPC")"
echo

# --- The compatibility claim docs/06 asks for ---

echo "reputation registry implements ERC-8004:"
REP_IMPL=0x$(cast storage "$REPUTATION" "$IMPL_SLOT" --rpc-url "$RPC" | tail -c 41)
CODE=$(cast code "$REP_IMPL" --rpc-url "$RPC")
for sig in \
  "giveFeedback(uint256,int128,uint8,string,string,string,string,bytes32)" \
  "readFeedback(uint256,address,uint64)" \
  "readAllFeedback(uint256,address[],string,string,bool)"; do
  sel=$(cast sig "$sig")
  if echo "$CODE" | grep -q "${sel:2}"; then
    echo "  ok       $sel  $sig"
  else
    echo "  MISSING  $sel  $sig"
    fail=1
  fi
done
echo

# --- Chainlink CRE ---

echo "chainlink CRE:"
if has_code "$CRE_FORWARDER"; then
  echo "  ok       forwarder $CRE_FORWARDER ($(code_size "$CRE_FORWARDER") bytes)"
else
  echo "  MISSING  forwarder $CRE_FORWARDER"
  echo "           per-network address; set CRE_FORWARDER for another chain."
  fail=1
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "one or more dependencies are not usable on this network"
  exit 1
fi
echo "all external dependencies verified on chain (chain $CHAIN_ID, $TIER registries)"

# Machine-readable, so a deployment manifest records what was verified
# rather than what someone believed.
cat <<JSON > "${OUT:-/dev/stdout}"
{
  "chain_id": $CHAIN_ID,
  "erc8004_tier": "$TIER",
  "identity_registry": "$IDENTITY",
  "reputation_registry": "$REPUTATION",
  "cre_forwarder": "$CRE_FORWARDER"
}
JSON
