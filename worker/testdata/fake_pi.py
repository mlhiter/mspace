#!/usr/bin/env python3
import json
import os
import sys


args = sys.argv[1:]
if args[:2] != ["--mode", "rpc"]:
    print("unexpected fake Pi args: " + " ".join(sys.argv[1:]), file=sys.stderr)
    sys.exit(2)
if "--append-system-prompt" in args:
    prompt_index = args.index("--append-system-prompt") + 1
    if prompt_index >= len(args) or not args[prompt_index]:
        print("missing fake Pi system prompt", file=sys.stderr)
        sys.exit(2)


def emit(payload):
    print(json.dumps(payload, separators=(",", ":")), flush=True)


mode = os.environ.get("MSPACE_FAKE_PI_MODE", "success")
if mode == "unsafe_session":
    print("Pi session file: /tmp/private/pi-session.json", file=sys.stderr, flush=True)
for line in sys.stdin:
    request = json.loads(line)
    command = request.get("type")
    if command == "get_state":
        session_id = "pi-session-opaque"
        if mode == "unsafe_session":
            session_id = "/tmp/private/pi-session.json"
        emit({"id": request.get("id"), "type": "response", "command": "get_state", "success": True, "data": {"sessionId": session_id, "sessionFile": "/tmp/private/pi-session.json"}})
    elif command == "prompt":
        if not request.get("message"):
            emit({"id": request.get("id"), "type": "response", "command": "prompt", "success": False, "error": "missing prompt"})
            continue
        emit({"id": request.get("id"), "type": "response", "command": "prompt", "success": True})
        if mode in ("success", "unsafe_session"):
            emit({"type": "message_end", "message": {"role": "assistant", "content": [{"type": "text", "text": "fake Pi completed"}]}})
            emit({"type": "agent_end", "messages": [{"role": "assistant", "content": [{"type": "text", "text": "fake Pi completed"}]}]})
        elif mode == "missing_agent_end":
            sys.exit(0)
        elif mode == "slow":
            ready = os.environ.get("MSPACE_FAKE_PI_READY")
            if ready:
                with open(ready, "w", encoding="utf-8") as handle:
                    handle.write("ready\n")
        else:
            print("unknown fake Pi mode: " + mode, file=sys.stderr)
            sys.exit(2)
    elif command == "abort":
        marker = os.environ.get("MSPACE_FAKE_PI_ABORT_MARKER")
        if marker:
            with open(marker, "w", encoding="utf-8") as handle:
                handle.write(json.dumps(request, separators=(",", ":")) + "\n")
        emit({"id": request.get("id"), "type": "response", "command": "abort", "success": True})
        sys.exit(0)
    else:
        emit({"id": request.get("id"), "type": "response", "command": command, "success": False, "error": "unknown command"})
