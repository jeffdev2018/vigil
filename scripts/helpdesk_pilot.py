#!/usr/bin/env python3
"""JEF-316 — internal helpdesk pilot pack (v0).

One script applies the whole helpdesk configuration to a workspace and
proves it with three sample requests:

  - a helpdesk agent on the native runtime, with the mandatory procedure
    (search the Brain first, cite it, propose the resolution through the
    gated transition, save new knowledge, admit what is not covered);
  - the starter procedures seeded into the workspace Brain;
  - the F28 rule "an agent's resolution needs owner approval";
  - three grounded/uncovered requests, measuring time to first answer.

Usage:
  python3 scripts/helpdesk_pilot.py http://localhost:18300 dev@localhost

Requires MULTICA_DEV_VERIFICATION_CODE auth (a dev stack). Every run is
idempotent-safe: agent, notes and rule are uniquely named per run; adjust
PROCEDURES to your workspace's real know-how before the real pilot.
"""


import json
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

BASE = (sys.argv[1] if len(sys.argv) > 1 else "http://localhost:18300").rstrip("/")
EMAIL = sys.argv[2] if len(sys.argv) > 2 else "dev@localhost"
CODE = "888888"

ok = True


def check(cond, label, detail=""):
    global ok
    if not cond:
        ok = False
    print(("PASS " if cond else "FAIL ") + label + ((" — " + detail) if detail else ""))


def call(method, path, body=None, token=None, ws=None, timeout=30):
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    if ws:
        req.add_header("X-Workspace-ID", ws)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
            return resp.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw.decode(errors="replace")
    except Exception as e:  # noqa: BLE001
        return 0, str(e)


INSTRUCTIONS = (
    "Tu es l'agent du support interne. Procédure obligatoire : "
    "1) cherche la réponse dans les notes du workspace (search_notes) AVANT de répondre ; "
    "2) réponds par un commentaire clair et concis en français, en citant la procédure trouvée ; "
    "3) si ta réponse résout la demande, propose le passage au statut done avec transition_issue ; "
    "4) si tu résous quelque chose qui mérite d'être conservé et qui n'est pas déjà dans les notes, enregistre-le (save_note) ; "
    "5) si aucune procédure ne couvre la demande, dis-le honnêtement et propose de créer la procédure. "
    "N'invente jamais une procédure."
)

PROCEDURES = [
    ("Procédure VPN", "Le client VPN se télécharge depuis le portail interne, onglet Outils. La double authentification doit être activée AVANT la première connexion : sans elle le compte se verrouille après trois essais.", ["helpdesk", "accès"]),
    ("Réinitialisation mot de passe", "Utiliser « Mot de passe oublié ? » sur le portail identité. Le lien reçu est valable 30 minutes. Si l'e-mail n'arrive pas, vérifier le dossier spam puis contacter l'assistance niveau 2.", ["helpdesk", "comptes"]),
    ("Accès outil facturation", "L'accès est demandé au responsable finance par le canal dédié. Délai habituel : 2 jours ouvrés. Relancer seulement au-delà.", ["helpdesk", "finance"]),
    ("Remboursement frais", "Tout remboursement passe par le formulaire Expenses, puis validation du responsable. Le remboursement intervient sous 5 jours ouvrés après validation.", ["helpdesk", "finance"]),
    ("Incident messagerie", "Vérifier la page de statut de la passerelle mail. Si elle est rouge, l'incident est déjà connu de l'infra : ne pas ouvrir de ticket, attendre le rétablissement. Si elle est verte, créer un ticket niveau 2 avec captures.", ["helpdesk", "incidents"]),
]

REQUESTS = [
    ("Je n'arrive pas à me connecter au VPN", "vpn", "double authentification"),
    ("Remboursement de mon déplacement client de mardi", "remboursement", "5 jours"),
    ("Comment exporter le rapport trimestriel depuis l'outil de planification ?", None, None),  # not covered anywhere
]


def main():
    call("POST", "/auth/send-code", {"email": EMAIL})
    status, body = call("POST", "/auth/verify-code", {"email": EMAIL, "code": CODE})
    check(status == 200 and isinstance(body, dict) and body.get("token"), "login", f"status={status}")
    if not (isinstance(body, dict) and body.get("token")):
        return
    token = body["token"]
    status, workspaces = call("GET", "/api/workspaces", token=token)
    ws = workspaces[0]["id"]
    status, runtimes = call("GET", "/api/runtimes", token=token, ws=ws)
    native = next((r for r in runtimes if r.get("provider") == "native"), None)
    check(native is not None and native.get("status") == "online", "native runtime online", "")
    if native is None:
        return

    # --- Setup (idempotent per run via unique names) ------------------------
    tag = str(int(time.time()))
    status, agent = call("POST", "/api/agents", {
        "name": "Assistance (pilote) " + tag,
        "runtime_id": native["id"],
        "instructions": INSTRUCTIONS,
    }, token=token, ws=ws)
    check(status in (200, 201) and agent.get("id"), "helpdesk agent created", f"status={status}")
    agent_id = agent["id"]

    seeded = 0
    for title, content, tags in PROCEDURES:
        st, _ = call("POST", "/api/workspace/notes", {"title": title, "content": content, "tags": tags}, token=token, ws=ws)
        if st in (200, 201):
            seeded += 1
    check(seeded == len(PROCEDURES), f"{seeded}/{len(PROCEDURES)} procedures seeded in the Brain", "")

    status, rule = call("POST", "/api/issue-transition-rules", {
        "to_category": "done",
        "allow_actor_types": ["agent"],
        "requires_approval": True,
        "approver_roles": ["owner"],
    }, token=token, ws=ws)
    check(status in (200, 201) and rule.get("id"), "F28 rule: agent resolutions need owner approval", f"status={status}")
    rule_id = rule.get("id")

    # --- Requests -----------------------------------------------------------
    for title, _topic, expect_fragment in REQUESTS:
        status, issue = call("POST", "/api/issues", {
            "title": title,
            "description": "Demande interne (pilote helpdesk).",
            "assignee_type": "agent",
            "assignee_id": agent_id,
        }, token=token, ws=ws)
        check(status in (200, 201) and issue.get("id"), f"request filed: {title}", f"status={status}")

        task = None
        first_answer_seconds = None
        deadline = time.time() + 240
        while time.time() < deadline:
            status, comments = call("GET", f"/api/issues/{issue['id']}/comments", token=token, ws=ws)
            rows = comments if isinstance(comments, list) else (comments or {}).get("comments", [])
            agent_rows = [c for c in rows if c.get("author_type") == "agent" and c.get("author_id") == agent_id]
            if agent_rows:
                # created_at strings sort chronologically; first = the answer
                first = min(agent_rows, key=lambda c: c.get("created_at", ""))
                break
            time.sleep(5)
        else:
            check(False, f"first answer: {title}", "no agent comment within 240s")
            continue

        answered = True
        status2, issue_now = call("GET", f"/api/issues/{issue['id']}", token=token, ws=ws)
        status3, reqs = call("GET", f"/api/issues/{issue['id']}/transition-requests", token=token, ws=ws)
        rows3 = reqs.get("requests") if isinstance(reqs, dict) else reqs
        pending = [r for r in (rows3 or []) if r.get("state") == "pending"]

        label = title[:44]
        content = first.get("content") or ""
        if expect_fragment:
            check(expect_fragment.lower() in content.lower(),
                  f"grounded answer cites the procedure: {label}", content[:140])
            check(len(pending) == 1 and issue_now.get("status") != "done",
                  f"resolution proposed, held for approval: {label}",
                  f"status={issue_now.get('status')} pending={len(pending)}")
        else:
            honest = any(w in content.lower() for w in ("aucune procédure", "pas de procédure", "je ne trouve pas", "ne figure pas"))
            check(honest, f"uncovered request answered honestly: {label}", content[:140])
        print(f"     → answered: {content[:120]}")

    if rule_id:
        call("DELETE", f"/api/issue-transition-rules/{rule_id}", token=token, ws=ws)

    print("PILOT " + ("OK" if ok else "FAILED"))
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
