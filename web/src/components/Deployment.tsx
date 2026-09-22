import { useEffect, useState } from "react";
import { loadManifest, type Manifest } from "../lib/data";

/**
 * Live truth in the chrome.
 *
 * Lending interfaces carry a strip that says what chain they are on and
 * what is verified, because a reader deciding whether to trust a number
 * wants to know where it came from without hunting. This is that strip.
 *
 * The difference is what it is allowed to say. The usual version of this
 * pattern is a green dot and the word "verified", which asserts a
 * property nobody checked. Everything here is read from the bundle
 * manifest -- the chain the contracts are deployed to, the deployment
 * ordinal, the ERC-8004 agent id -- and if the manifest is absent the
 * strip renders NOTHING rather than a reassuring default.
 *
 * Specifically not claimed: that the network is live right now. This is a
 * static bundle, so a pulsing "live" dot would be theatre. The dot marks
 * a deployment that exists, and the label says `testnet`, because that is
 * what chain 10143 is.
 */

/** Chains this project actually deploys to. */
const CHAINS: Record<number, string> = {
  10143: "Monad testnet",
};

function chainName(id: number | undefined): string | null {
  if (id === undefined) return null;
  return CHAINS[id] ?? `chain ${id}`;
}

export function Deployment() {
  const [manifest, setManifest] = useState<Manifest | null>(null);

  useEffect(() => {
    let live = true;
    loadManifest()
      .then((m) => live && setManifest(m))
      // No bundle, no strip. A provenance line that survives its own
      // data going missing is decoration.
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, []);

  const d = manifest?.deployment;
  const chain = chainName(d?.chain_id);
  if (!d || !chain) return null;

  const contracts = d.addresses ? Object.keys(d.addresses).length : 0;
  const agent = d.erc8004?.agent_id;

  return (
    <div className="deployment">
      <span className="deployment-item">
        <span className="deployment-dot" aria-hidden="true" />
        {chain}
      </span>
      {contracts > 0 && (
        <span className="deployment-item">{contracts} contracts deployed</span>
      )}
      {agent !== undefined && (
        <span className="deployment-item">
          ERC&#8209;8004 agent <span className="deployment-figure">{agent}</span>
        </span>
      )}
      {d.deployment_id !== undefined && (
        <span className="deployment-item deployment-quiet">
          deployment {d.deployment_id}
        </span>
      )}
    </div>
  );
}
