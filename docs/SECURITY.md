# 🔐 Security & Trust Model

This document states plainly what this system does and does not protect, so operators and users
can make informed decisions. It is descriptive, not aspirational — every claim below was checked
against the source.

---

## ⚠️ The single most important thing to understand

> **Any prompt that is dispatched to a remote node is readable in plaintext by the operator of
> that node.**

This is not a bug or a missing feature. It is an unavoidable consequence of how inference works:
to generate a completion, a GPU must process the actual text. No practical technology today
allows an LLM to run on encrypted text (homomorphic encryption is not viable at this scale).

If a prompt would be damaging for a stranger to read, **do not send it through a swarm containing
machines you do not control.**

---

## How a request actually travels

```
User ──HTTP──> Node A gateway (:50006)
                     │
                     ├── local GPU is free ──> Node A's own vLLM        (stays on A)
                     │
                     └── local GPU is busy ──> libp2p stream ──> Node B
                                                                   │
                                              plaintext here ──────┤
                                                                   ▼
                                                            Node B's vLLM (:8100)
```

When Node A's GPU is already busy, the dispatcher forwards the request to a peer
(`proxy.go: streamToPeer`). What crosses the wire is the user's request body, unmodified:

```go
reqBytes, _ := json.Marshal(reqData)   // the full request, including message content
req, _ := http.NewRequestWithContext(ctx, "POST", path, bytes.NewReader(reqBytes))
req.Write(stream)
```

Node B receives it, reconstructs the HTTP request, and hands it to its own local vLLM
(`p2p.go`, `/mooncake-proxy/1.0.0` handler):

```go
req, err := http.ReadRequest(bufio.NewReader(s))    // full plaintext in Node B's memory
req.URL.Host = fmt.Sprintf("127.0.0.1:%d", targetPort)
resp, err := client.Do(req)                          // forwarded to B's vLLM
resp.Write(s)                                        // response also passes through B
```

Node B's operator needs no special tooling to read this. The traffic between the agent and its
own vLLM is **unencrypted HTTP on loopback**, and the operator controls the process, the logs,
and the source code.

---

## What transport encryption does and does not cover

The swarm uses libp2p with a pre-shared key (`p2p.go`: `libp2p.PrivateNetwork(psk)`) plus
libp2p's default Noise handshake.

| Threat | Protected? |
| :--- | :---: |
| Someone sniffing traffic **between** two nodes | ✅ Yes — Noise-encrypted |
| Someone without `swarm.key` joining the mesh | ✅ Yes — PSK-gated |
| **The receiving node itself reading your prompt** | ❌ **No** |

Transport encryption protects data *in transit from third parties*. The receiving node is the
intended recipient — it must decrypt to do its job. Encryption is not a defence against the
endpoint.

There is **no application-layer encryption** anywhere in this codebase. That is a deliberate
statement of fact, not an oversight to be worked around.

---

## What a remote node can and cannot observe

| Data | Visible to the receiving node? |
| :--- | :--- |
| Full prompt / message content | ✅ Plaintext |
| Model response content | ✅ Plaintext (the response is relayed back through it) |
| Sender's libp2p PeerID | ✅ (`s.Conn().RemotePeer()`) |
| Sender's IP address | ⚠️ Usually — a PeerID maps to its multiaddr |
| Original `Authorization` / custom headers | ❌ The P2P path sets only `Content-Type`; original headers are not forwarded |

> [!NOTE]
> The header behaviour differs by path: a request served **locally** forwards the client's
> headers verbatim (`req.Header = r.Header.Clone()`), while a request forwarded **over P2P**
> does not. So API keys sent to your own node are not leaked to peers — but the prompt is.

---

## The actual trust model

`swarm.key` is an **admission control** mechanism, not an isolation mechanism.

- It decides **who may join** the mesh.
- It does **not** constrain what a member may do with traffic it receives.

Once a node holds a valid `swarm.key`, it is a fully trusted peer. The operating assumption is
therefore:

> **Every operator of every node in the swarm is trusted not to read, retain, or misuse the
> prompts routed to them.**

That assumption is reasonable for a swarm of machines under one organisation. It is **not**
reasonable for an open network that anyone may join. Treat `swarm.key` distribution with the
same seriousness as granting someone read access to your users' data — because that is what it
does.

---

## Guidance for operators

**Running a private/organisational swarm (the intended deployment):**
- Distribute `swarm.key` only to machines you or your organisation control.
- Never commit a real `swarm.key`; never use `swarm.key.example` as a real key (it is public).
- Rotate the key if a node leaves your control — see [`P2P_NETWORK.md`](P2P_NETWORK.md).

**Considering an open swarm:**
- Understand that every participant can read every prompt routed to them.
- Tell your users this, explicitly, before they type anything.

**Handling sensitive data:**
- Don't route it through nodes you don't control.
- If a node must never export prompts, keep it out of the mesh or ensure it always serves
  locally.

---

## Options if you need stronger guarantees

Listed with honest trade-offs; none is currently implemented.

| Approach | Effect | Cost |
| :--- | :--- | :--- |
| **Policy + disclosure** | Users make informed choices | No technical guarantee |
| **Local-only privacy mode** | Sensitive requests never leave the machine | Loses distributed throughput for those requests |
| **PeerID allowlist for dispatch** | Only forward to specifically trusted nodes | Reverts to invite-only; conflicts with open participation |
| **Confidential computing (TEE)** | The only real technical fix — operator cannot read GPU memory | Requires supported hardware (e.g. H100 CC mode) and significant work |

---

## Hardening already in place

These were added after a security review and are verified by live testing:

- **Proxy tunnel port allowlist** — the `/mooncake-proxy/1.0.0` handler previously forwarded to
  *any* localhost port on request. Combined with Ray's unauthenticated dashboard, any peer could
  have achieved remote code execution on any node. Now restricted to the vLLM and Mooncake
  bootstrap ports, with rejections logged durably.
- **No shell interpolation in the vLLM launch path** — config values are passed as argv, so
  config content cannot become shell commands.
- **Container hardening** — `cap_drop: ALL`, `no-new-privileges`, `pids_limit`, and read-only
  mounts for files the agent never writes.

See [`docs/test/WINDOWS_NATIVE_TEST.md`](test/WINDOWS_NATIVE_TEST.md) for the verification runs.

---

## Known unauthenticated surfaces

Deliberately documented rather than silently patched, because fixing them is a design decision:

| Surface | Note |
| :--- | :--- |
| Web dashboard + `/api/*` (`:50007`) | **No authentication.** Includes `POST /api/config`, which rewrites that node's configuration. Anyone who can reach the port controls the node's settings. Bind it to a trusted network. |
| OpenAI gateway (`:50006`) | No authentication — anyone who can reach it can consume your GPU. |
| vLLM (`:8100`) | No auth. The native Windows path binds `--host 0.0.0.0` explicitly; on Linux the container runs with `network_mode: host`, so vLLM's port sits on the host's network rather than an isolated container network. Verify your own exposure with `ss -lntp \| grep 8100`. |

A node's configuration only affects **that node** — there is no mechanism for one node to alter
another's config. The port allowlist above is enforced using each node's *own* configuration, so
a compromised peer cannot redirect a victim node's traffic.

---

## Reporting a vulnerability

Open an issue at
[lhu-csie-dclab/yuanyi/issues](https://github.com/lhu-csie-dclab/yuanyi/issues). This is
experimental research software with no security SLA — see the disclaimer in the
[README](../README.md).
