"""Record a Kuru listing in the deployment manifest.

Its own file rather than a heredoc inside `list-notes-kuru.sh`: that
script already nests one, and a second collides with it.

The addresses used to survive only in the script's stdout and then in
`docs/08` prose. `README.md` says the manifest is the only place
addresses are recorded, for a reason -- three prose copies had already
gone stale by 21 September.
"""

import collections
import json
import sys


def main() -> None:
    path, router, market, note, settlement, impl = sys.argv[1:7]

    with open(path) as f:
        manifest = json.load(f, object_pairs_hook=collections.OrderedDict)

    manifest["kuru"] = collections.OrderedDict(
        [
            ("router", router),
            ("market", market),
            ("note", note),
            ("settlement", settlement),
            ("order_book_implementation", impl),
            # The caveat travels WITH the addresses. A reader who finds
            # a market in a manifest and no qualification beside it will
            # reasonably assume the market priced something.
            (
                "status",
                "listed and verified by the router; CONTROLLED market -- both "
                "participants are project-controlled, so this shows the venue "
                "accepts the instrument, not that anyone independent priced it",
            ),
            (
                "isolated_from_baseline",
                "the series has its own settlement token, not addresses.settlement: "
                "docs/08 forbids tokenizing a receivable of the baseline pooled vault",
            ),
        ]
    )

    with open(path, "w") as f:
        json.dump(manifest, f, indent=2)
        f.write("\n")

    print(f"  recorded in contracts/deployments/{manifest['chain_id']}.json")


if __name__ == "__main__":
    main()
