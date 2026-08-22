[English](USER_NOTICE.md) | [繁體中文](zh_tw/USER_NOTICE.md) | [简体中文](zh_cn/USER_NOTICE.md)

# 📋 User Notice — Read Before Joining a Swarm

> [!WARNING]
> **This is experimental research software.** Everything below describes how it behaves today.
> There are no privacy guarantees, no security SLA, and no warranty. Do not use it for anything
> you cannot afford to have exposed.

Joining a swarm means sharing a machine with other people. This page states plainly what you
give up and what you take on, so you can decide before you join — not after.

---

## 1. Your machine broadcasts telemetry to every other node, every 3 seconds

Every node continuously gossips its status to the entire swarm. This is not optional and cannot
be turned off. Every other member receives all of it.

| Broadcast | What it reveals |
| :--- | :--- |
| `addr` — your libp2p multiaddress | **Your IP address**, and therefore your approximate location and ISP |
| `node_id` — your PeerID | A **stable identifier that survives restarts** (it is saved in `identity.key`), so your activity can be correlated over time |
| `summary`, `vram_total`, `driver_version` | Your exact **GPU model, VRAM size and driver version** — a usable hardware fingerprint |
| `gpu_temp`, `gpu_util`, `power_draw`, `power_limit`, `fan_speed` | Live hardware telemetry |
| `total_requests`, `total_tokens`, `in_tokens`, `out_tokens` | **How much you use the system** |
| `status`, `active_requests`, `timestamp` | **When your machine is active** — over time this reveals your usage schedule |
| `gen_speed`, `prefill_speed`, `avg_ttft`, `kv_cache_usage` | Performance characteristics |

**This data is also stored and published:**

- Any node running in **hub mode** writes your PeerID, **IP address** and telemetry into a local
  SQLite database (`peers.db`) and keeps it. You cannot see who is doing this or ask them to
  delete it.
- Hub nodes publish a **public contribution leaderboard** (`top.json`, and a dashboard page)
  ranking participants by GPU capability.

Prompt *content* is never included in this telemetry — but everything above is.

## 2. You receive everyone else's telemetry too

The exchange is symmetric. Your node collects the same data about every other participant, and
stores it locally. If you run a hub, you are persisting other people's IP addresses to disk —
which may carry obligations under privacy law in your jurisdiction (GDPR, PIPA, 個資法, etc.).
Consider that before enabling hub mode.

## 3. Your prompts may leave your computer in plaintext

When your local GPU is busy, your request is dispatched to **another person's machine**, which
must decrypt it to run inference. That operator can read your prompt and the model's response in
full.

Transport encryption protects your data from third parties on the network — it does **not**
protect it from the node that runs it. This is unavoidable: inference requires plaintext.

👉 Details and the full trust model: **[`SECURITY.md`](SECURITY.md)**

**Therefore: never type anything into a shared swarm that you would not be willing to show to
every other operator in it.** Passwords, personal data, medical or financial details, proprietary
code, unpublished research — keep them out.

## 4. Other people's prompts will run on your GPU

The reverse is equally true, and easy to overlook:

- **You cannot control what content other participants send through your hardware.** It may be
  offensive, or illegal in your country. It will be processed by your GPU, on your electricity,
  under your IP address.
- **Your hardware is consumed by others.** Expect power draw, heat, and wear whenever your node
  is idle enough to accept work.
- If you are subject to institutional rules about what your equipment may process, joining a
  public swarm may violate them. Check first.

## 5. Sharing `swarm.key` is the entire security boundary

`swarm.key` decides **who may join**. It does **not** limit what a member may do once inside.
Handing someone the key gives them, permanently until you rotate it:

- The ability to read every prompt routed to them
- Your IP address, hardware fingerprint and usage patterns
- The ability to consume your GPU
- The ability to feed arbitrary content into your hardware

**Treat sharing `swarm.key` as equivalent to granting read access to your users' data**, because
functionally that is what it does.

Before sharing a key, ask:

- Do I actually trust this person and their machine's security?
- What happens if their machine is compromised — or if they leave?
- Do I have a plan to rotate the key? (Rotation requires updating **every** node.)
- Am I allowed to share it, under my organisation's rules?

## 6. If you are using a public or widely shared key

Assume **everything is visible to strangers**:

- ✅ Fine: benchmarking, testing, public/synthetic data, throwaway prompts
- ❌ Not fine: personal information, credentials, client or patient data, proprietary content,
  anything confidential

Also be aware that the dashboard (`:50007`), the API gateway (`:50006`) and vLLM (`:8100`) have
**no authentication**. Anyone who can reach those ports on your machine can use your GPU and read
your node's configuration and logs. Do not expose them to the open internet — see
[`SECURITY.md`](SECURITY.md).

## 7. If any of this is unacceptable — you can still use the software

You do not have to join a shared swarm to benefit from this project:

- **Run standalone.** A node with no peers still serves inference from its own GPU. Nothing is
  dispatched anywhere, and nothing is broadcast to anyone.
- **Contribute as a relay only.** Set `server_mode.relay_only: true` to join and help the network without running inference. No GPU is used, and **other people's prompts never execute on your hardware** — a circuit relay forwards the *encrypted* stream and cannot read it. This removes §4 entirely and most of §3. See [`HUB_MODE.md`](HUB_MODE.md).
- **Run a private swarm.** Use a key shared only among machines you or your organisation control.
  Every risk above then applies only within a trust boundary you already accept.

The risks in this document are specific to **sharing a swarm with people you do not control**.

---

## Summary

| | |
| :--- | :--- |
| Your IP, GPU model and usage patterns | 📡 Broadcast to all peers every 3s, stored by hubs |
| Your prompts | ⚠️ Readable by whichever node executes them |
| Other people's prompts | ⚠️ Will execute on your GPU, content outside your control |
| `swarm.key` | 🔑 Admission control only — members are fully trusted |
| Dashboard / gateway / vLLM ports | 🔓 No authentication |
| Maturity | 🧪 Experimental — no guarantees of any kind |

**Do not share private or sensitive information through a swarm you do not fully control.**
