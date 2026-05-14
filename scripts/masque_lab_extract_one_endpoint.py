"""Extract one MASQUE client endpoint from masque-auth-lab-client.json for isolated HiddifyCli tests."""
import json
import sys


def main() -> None:
    if len(sys.argv) != 4:
        print("usage: masque_lab_extract_one_endpoint.py <lab-client.json> <endpoint_tag> <out.json>", file=sys.stderr)
        sys.exit(2)
    lab_path, tag, out_path = sys.argv[1], sys.argv[2], sys.argv[3]
    with open(lab_path, encoding="utf-8") as f:
        lab = json.load(f)
    ep = next((e for e in lab.get("endpoints", []) if e.get("tag") == tag), None)
    if ep is None:
        print(f"tag not found: {tag}", file=sys.stderr)
        sys.exit(1)
    out = {
        "endpoints": [ep],
        "outbounds": lab.get("outbounds", []),
        "route": {"final": tag},
    }
    with open(out_path, "w", encoding="utf-8") as f:
        json.dump(out, f, indent=2)
        f.write("\n")


if __name__ == "__main__":
    main()
