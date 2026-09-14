#!/usr/bin/env bash
# Verify the external dependencies exist on the target network.
#
# `docs/06` requires the registry address AND compatibility evidence in
# the deployment manifest. Documentation is not evidence: this checks the
# deployed bytecode, so a moved or undeployed contract fails loudly rather
# than at demo time.
set -euo pipefail

RPC=${RPC:-https://testnet-rpc.monad.xyz}

# Testnets use the 0x8004A818 / 0x8004B663 vanity addresses; the
# 0x8004A169 / 0x8004BAa1 pair is MAINNET and is empty here.
IDENTITY=${IDENTITY:-0x8004A818BFB912233c491871b3d84c89A494BD9e}
REPUTATION=${REPUTATION:-0x8004B663056A597Dffe9eCcC1965A193B7388713}
CRE_FORWARDER=${CRE_FORWARDER:-0xF8344CFd5c43616a4366C34E3EEE75af79a74482}

# EIP-1967 implementation slot: these registries are proxies.
IMPL_SLOT=0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc

fail=0

echo "chain id: $(cast chain-id --rpc-url "$RPC")"
echo

check_deployed() {
  local label=$1 addr=$2
  local size
  size=$(cast code "$addr" --rpc-url "$RPC" | wc -c | tr -d ' ')
  if [ "$size" -le 2 ]; then
    echo "  MISSING  $label $addr"
    fail=1
  else
    echo "  ok       $label $addr ($((size/2)) bytes)"
  fi
}

echo "deployed:"
check_deployed "identity registry  " "$IDENTITY"
check_deployed "reputation registry" "$REPUTATION"
check_deployed "CRE forwarder      " "$CRE_FORWARDER"
echo

echo "identity registry is an ERC-721 agent registry:"
echo "  name   $(cast call "$IDENTITY" 'name()(string)' --rpc-url "$RPC")"
echo "  symbol $(cast call "$IDENTITY" 'symbol()(string)' --rpc-url "$RPC")"
echo

# The compatibility claim `docs/06` asks for: the deployed implementation
# carries the selectors the standard defines.
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
if [ "$fail" -ne 0 ]; then
  echo "one or more dependencies are not usable on this network"
  exit 1
fi
echo "all external dependencies verified on chain"
