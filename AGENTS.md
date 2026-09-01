# Agent Rules — FanControl

Rules for any AI agent working in this repository. These override convenience
and prior habits. Break them only when the user explicitly asks in the moment.

## 1. Do exactly what was asked — nothing more

- Work only on the specific task the user requested.
- Anything outside that task needs the user's explicit confirmation **first**,
  before you do it — not after, and not as part of "finishing the job".

## 2. Ask before every non-coding action

These all need confirmation **every single time**, even if the user said yes to
the same thing before:

- `git commit`, `git push`, force-push, branch operations, GitHub operations
  (issues, PRs, releases, repo settings)
- Deploying, installing, or starting anything on any machine
- Networking/remote access of any kind (SSH, curl, probes) — this project's own
  servers are **never** to be connected to without an explicit direct request
- Running long builds, tests against live systems, or anything that changes
  state outside the working tree
- Touching files the user didn't ask about (rename, move, delete, reformat)

## 3. Permission never carries over

- A yes from yesterday is not a yes today. Even "you can push this" or "go
  ahead" in a previous message does **not** mean the next push is approved.
- Always ask, in the current message, before doing the action again.
- When in doubt about whether an action is in scope: **ask before acting.**

## 4. Coding tasks

- Editing project source, configs, and docs to complete the requested task is
  in scope.
- Local verification needed to complete the task (type-check, build, unit
  tests) is in scope.
- Sharing code with the world (commit/push) is **not** part of a coding task —
  it needs its own confirmation.

## 5. When you finish

- Summarize what changed and what you verified.
- If a commit/push is still pending, say so and ask whether to do it.
