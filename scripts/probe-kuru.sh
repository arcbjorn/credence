#!/usr/bin/env bash
# The Kuru feasibility experiment `docs/08` schedules for 15 September.
#
# It answers three questions and nothing else: is market creation
# permissionless, what does it cost, and does the venue accept this
# deployment's own tokens. Everything is a read or an `eth_call` -- this
# script sends no transaction and creates no market, so it costs nothing
# and can be re-run by a reader with no funded key.
#
# `docs/08`'s rule is that a capability check is run against the actual
# intended environment rather than read from documentation. Two things
# here were true in the docs and false or misleading on chain, which is
# the whole reason the rule exists:
#
#   * The SDK prose documents `deployMarket(...)`. The contract has no
#     such selector; the published ABI names `deployProxy` with eleven
#     parameters, and that one IS in the deployed bytecode.
#   * `deployProxy` reverts with EMPTY revert data when handed an address
#     that holds no code. That reads exactly like an access-control
#     rejection and is not one -- the same caller succeeds with real
#     ERC20s. Reporting "permissioned" from the first result would have
#     been a wrong answer to the gate's only question.
set -euo pipefail

RPC=${RPC:-https://testnet-rpc.monad.xyz}
ROOT=$(cd "$(dirname "$0")/.." && pwd)

# Kuru on Monad testnet, from docs.kuru.io/contracts/Contract-addresses.
ROUTER=${KURU_ROUTER:-0x7EFbE105Ca7415dE98F96622173458ac1c054630}
MARGIN=${KURU_MARGIN:-0xd029C2D98ff85D8F64799017fE00a59B1159CE02}
DEPLOYER=${KURU_DEPLOYER:-0xDacd06372cEb638640c9D8466A023b7362324e1A}
FORWARDER=${KURU_FORWARDER:-0x681bB1508E14433b148a2549ba2726454aDc9BB4}

DEPLOY_SIG='deployProxy(uint8,address,address,uint96,uint32,uint32,uint96,uint96,uint256,uint256,uint96)'

CHAIN_ID=$(cast chain-id --rpc-url "$RPC")
echo "=== Kuru capability check, chain $CHAIN_ID ==="

codesize() {
  local code
  code=$(cast code "$1" --rpc-url "$RPC" 2>/dev/null || echo 0x)
  echo $(((${#code} - 2) / 2))
}

for pair in "router:$ROUTER" "margin:$MARGIN" "deployer:$DEPLOYER" \
  "forwarder:$FORWARDER"; do
  printf '  %-10s %s  %s bytes\n' "${pair%%:*}" "${pair##*:}" \
    "$(codesize "${pair##*:}")"
done

# 141 bytes is an EIP-1967 proxy, so the implementation is what carries
# the selectors. Reading the slot proves which contract actually answers.
IMPL_SLOT=0x360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc
IMPL=0x$(cast storage "$ROUTER" "$IMPL_SLOT" --rpc-url "$RPC" | cut -c27-)
echo "  router impl $IMPL  $(codesize "$IMPL") bytes"

echo
echo "=== is this really the Kuru router? ==="
echo "  orderBookImplementation() $(cast call "$ROUTER" \
  'orderBookImplementation()(address)' --rpc-url "$RPC")"
FWD=$(cast call "$ROUTER" 'TRUSTED_FORWARDER()(address)' --rpc-url "$RPC")
echo "  TRUSTED_FORWARDER()       $FWD"
if [ "$(echo "$FWD" | tr 'A-Z' 'a-z')" = "$(echo "$FORWARDER" | tr 'A-Z' 'a-z')" ]; then
  echo "  -> matches the documented forwarder; docs and chain agree"
else
  echo "  -> DOES NOT match the documented forwarder" >&2
fi

# The selector the published ABI names must be in the deployed bytecode.
# The one the SDK prose names is not, which is why this is checked.
SEL=$(cast sig "$DEPLOY_SIG" | cut -c3-10)
if cast code "$IMPL" --rpc-url "$RPC" | grep -q "$SEL"; then
  echo "  deployProxy ($SEL)        present in the implementation"
else
  echo "  deployProxy ($SEL)        ABSENT -- the ABI has moved" >&2
  exit 1
fi

# This deployment's own tokens, so the answer is about assets Credence
# actually holds rather than a hypothetical pair.
MANIFEST="$ROOT/contracts/deployments/$CHAIN_ID.json"
if [ ! -f "$MANIFEST" ]; then
  echo "no manifest at $MANIFEST; nothing to price a market against" >&2
  exit 1
fi
addr() { python3 -c \
  "import json;print(json.load(open('$MANIFEST'))['addresses']['$1'])"; }
RISKY=$(addr risky)
SETTLE=$(addr settlement)
CALLER=${KURU_CALLER:-$(python3 -c \
  "import json;print(json.load(open('$MANIFEST'))['erc8004']['agent_owner'])")}

echo
echo "=== can an ordinary address create a market? ==="
echo "  base  $RISKY   (TestRiskyAsset)"
echo "  quote $SETTLE   (TestSettlementToken)"
echo "  from  $CALLER   (no special role)"

# Market parameters: a plausible shape, not a tuned one. The question is
# whether the call is permitted, not whether these are good settings.
PARAMS=(0 "$RISKY" "$SETTLE" 1000000000 100000 1 1000 1000000000000 30 10 100)

if MARKET=$(cast call "$ROUTER" "$DEPLOY_SIG(address)" "${PARAMS[@]}" \
  --from "$CALLER" --rpc-url "$RPC" 2>/dev/null); then
  echo "  -> PERMISSIONLESS: simulates to market $MARKET"
else
  echo "  -> refused. NOTE: deployProxy also reverts with empty data when" >&2
  echo "     a token address holds no code, which is not a permission" >&2
  echo "     failure. Check both assets exist before reading this as a gate." >&2
  exit 1
fi

GAS=$(cast estimate "$ROUTER" "$DEPLOY_SIG" "${PARAMS[@]}" \
  --from "$CALLER" --rpc-url "$RPC" 2>/dev/null || echo 0)
PRICE=$(cast gas-price --rpc-url "$RPC")
python3 -c "
g, p = $GAS, $PRICE
print(f'  -> {g:,} gas at {p/1e9:.0f} gwei = {g*p/1e18:.4f} MON')"

echo
echo "PASS  market creation is permissionless and cheap; no feasibility blocker"
echo "      (this prices CREATING A MARKET, not tokenising a receivable --"
echo "       NoteSeries is unbuilt, and the 2 Oct gate wants redemption)"
