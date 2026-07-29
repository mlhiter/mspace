#!/usr/bin/env python3
import json
import os
import sys


for forbidden in ("MSPACE_RUNTIME_TOKEN", "MSPACE_RUNTIME_TOKEN_FILE", "MSPACE_SERVER_URL", "DATABASE_URL", "GITHUB_TOKEN", "SENTRY_AUTH_TOKEN"):
    if os.environ.get(forbidden):
        print("control-plane or unrelated secret leaked through " + forbidden, file=sys.stderr)
        sys.exit(88)


args = sys.argv[1:]
required = ["-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"]
if any(value not in args for value in required):
    print("unexpected fake Claude Code args: " + " ".join(args), file=sys.stderr)
    sys.exit(2)

request_line = sys.stdin.readline()
if not request_line:
    print("missing stream-json input", file=sys.stderr)
    sys.exit(2)
request = json.loads(request_line)
if request.get("type") != "user" or not request.get("message", {}).get("content"):
    print("invalid stream-json input", file=sys.stderr)
    sys.exit(2)


def emit(payload):
    print(json.dumps(payload, separators=(",", ":")), flush=True)


mode = os.environ.get("MSPACE_FAKE_CLAUDE_MODE", "success")
expected_skill = os.environ.get("MSPACE_FAKE_CLAUDE_EXPECT_SKILL")
if expected_skill:
    skills_root = os.environ.get("MSPACE_SESSION_SKILLS_DIR")
    if not skills_root:
        print("missing MSPACE_SESSION_SKILLS_DIR", file=sys.stderr)
        sys.exit(89)
    expected_skill_path = os.path.join(skills_root, *expected_skill.split("/"))
    if not os.path.isfile(expected_skill_path):
        print("missing materialized skill: " + expected_skill_path, file=sys.stderr)
        sys.exit(89)
emit({"type": "system", "subtype": "init", "session_id": "claude-session-opaque"})
if mode == "success":
    emit({"type": "assistant", "session_id": "claude-session-opaque", "message": {"content": [{"type": "text", "text": "fake Claude completed"}]}})
    emit({"type": "result", "subtype": "success", "is_error": False, "session_id": "claude-session-opaque", "uuid": "claude-run-opaque", "result": "fake Claude final result"})
elif mode == "failure":
    emit({"type": "result", "subtype": "error", "is_error": True, "session_id": "claude-session-opaque", "result": "fake Claude failed"})
elif mode == "missing_result":
    emit({"type": "assistant", "session_id": "claude-session-opaque", "message": {"content": "no terminal result"}})
else:
    print("unknown fake Claude Code mode: " + mode, file=sys.stderr)
    sys.exit(2)
