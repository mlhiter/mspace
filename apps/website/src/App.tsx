import {
  ArrowUpRight,
  Box,
  CircleDot,
  ClipboardCheck,
  GitBranch,
  Network,
  ShieldCheck,
  TerminalSquare,
} from "lucide-react";
import type { CSSProperties } from "react";
import brandMark from "../../desktop/assets/brand/mspace-icon.png";
import issueDetailImage from "../../../docs/images/mspace-issue-detail.png";
import issuesListImage from "../../../docs/images/mspace-issues-list.png";

const workflow = [
  {
    step: "Issue",
    detail: "Problem statement, owner, comments, and child tasks stay in one document.",
  },
  {
    step: "Agent session",
    detail: "Codex runs in a prepared worktree with the current issue context.",
  },
  {
    step: "Code change",
    detail: "Branch, commit, changed files, and diff preview remain attached to the issue.",
  },
  {
    step: "K8s preview",
    detail: "The issue can reserve a namespace, deploy the change, and return a probed URL.",
  },
  {
    step: "Review evidence",
    detail: "Commands, tests, resources, logs, risks, and cleanup state become review material.",
  },
];

const proofRows = [
  ["branch", "mspace/issue-23/session-a7c", "commit captured"],
  ["namespace", "mspace-issue-23", "preview retained"],
  ["cluster", "hangzhou-test", "reachable"],
  ["evidence", "build, deploy, pod logs", "reviewable"],
];

const controlPoints = [
  {
    icon: ClipboardCheck,
    title: "Issue as the source of truth",
    body: "mspace keeps the prompt, comments, session state, branch output, and validation decision on the issue page.",
  },
  {
    icon: TerminalSquare,
    title: "Local runtime first",
    body: "Agent work starts in a local git worktree, so the first product loop stays fast and inspectable.",
  },
  {
    icon: Network,
    title: "Kubernetes as validation",
    body: "The test environment is an issue-scoped namespace with a preview URL and resource evidence.",
  },
  {
    icon: ShieldCheck,
    title: "Permission boundaries stay visible",
    body: "Cluster, namespace, kubeconfig, registry, and cleanup decisions are product data, not hidden chat context.",
  },
];

const quickStart = ["pnpm install", "pnpm dev:desktop", "pnpm dev:website"];

export function App() {
  const heroStyle = {
    "--hero-image": `url(${issueDetailImage})`,
  } as CSSProperties;

  return (
    <main className="site-shell">
      <section className="hero" style={heroStyle} aria-label="mspace overview">
        <header className="nav-rail" aria-label="Primary">
          <a className="brand-lockup" href="#top" aria-label="mspace home">
            <img src={brandMark} alt="" width="32" height="32" />
            <span>mspace</span>
          </a>
          <div className="case-strip" aria-label="Landing page case file">
            <span>case mspace-001</span>
            <span>issue to preview proof</span>
            <span>local MVP</span>
          </div>
          <a
            className="nav-action"
            href="https://github.com/mlhiter/mspace"
            target="_blank"
            rel="noreferrer"
          >
            GitHub
            <ArrowUpRight aria-hidden="true" size={16} strokeWidth={2} />
          </a>
        </header>

        <div className="hero-grid" id="top">
          <p className="specimen-label">Kubernetes-native issue workspace</p>
          <h1>
            Agents create output.
            <span>mspace creates evidence.</span>
          </h1>
          <p className="hero-copy">
            A desktop workspace plus control plane where coding agent work becomes an issue record:
            session, branch, namespace, preview URL, logs, and cleanup decision in one place.
          </p>
          <div className="hero-actions" aria-label="Hero actions">
            <a href="#quick-start" className="primary-link">
              Run locally
              <TerminalSquare aria-hidden="true" size={18} strokeWidth={2} />
            </a>
            <a href="#proof" className="secondary-link">
              Inspect the proof
              <ArrowUpRight aria-hidden="true" size={18} strokeWidth={2} />
            </a>
          </div>
        </div>

        <div className="hero-stamps" aria-label="Product proof points">
          <span>Issue namespace</span>
          <span>Codex app-server</span>
          <span>Diff preview</span>
          <span>Preview probe</span>
        </div>
      </section>

      <section className="ticker" aria-label="mspace loop summary">
        <span>Issue</span>
        <CircleDot aria-hidden="true" size={14} />
        <span>Agent session</span>
        <CircleDot aria-hidden="true" size={14} />
        <span>Code change</span>
        <CircleDot aria-hidden="true" size={14} />
        <span>K8s namespace</span>
        <CircleDot aria-hidden="true" size={14} />
        <span>Review evidence</span>
      </section>

      <section className="workflow-section" id="workflow">
        <div className="section-kicker">The loop mspace owns</div>
        <div className="workflow-head">
          <h2>Not another chat box around an agent.</h2>
          <p>
            Codex can edit code. The harder problem is keeping the work reviewable after the
            terminal scrollback is gone.
          </p>
        </div>

        <div className="workflow-board" aria-label="Issue to evidence workflow">
          {workflow.map((item, index) => (
            <article className="workflow-step" key={item.step}>
              <span className="step-number">{String(index + 1).padStart(2, "0")}</span>
              <h3>{item.step}</h3>
              <p>{item.detail}</p>
            </article>
          ))}
        </div>
      </section>

      <section className="proof-section" id="proof">
        <div className="proof-copy">
          <div className="section-kicker">The evidence surface</div>
          <h2>The issue page becomes a record a teammate can inherit.</h2>
          <p>
            The product center is not the agent transcript. It is the issue page that explains
            what changed, where it ran, what URL was probed, and what state should be cleaned.
          </p>
          <dl className="proof-ledger">
            {proofRows.map(([label, value, status]) => (
              <div key={label}>
                <dt>{label}</dt>
                <dd>{value}</dd>
                <span>{status}</span>
              </div>
            ))}
          </dl>
        </div>

        <figure className="screenshot-stage">
          <img
            src={issueDetailImage}
            alt="mspace issue detail page with activity, sessions, and evidence"
            width="1440"
            height="1100"
          />
          <button className="annotation annotation-branch" type="button" aria-label="Branch evidence">
            <GitBranch aria-hidden="true" size={16} />
            <span>branch and diff</span>
          </button>
          <button className="annotation annotation-env" type="button" aria-label="Namespace evidence">
            <Box aria-hidden="true" size={16} />
            <span>namespace preview</span>
          </button>
        </figure>
      </section>

      <section className="split-proof">
        <figure>
          <img
            src={issuesListImage}
            alt="mspace issues list with status, owner, and session metadata"
            width="1440"
            height="900"
            loading="lazy"
          />
        </figure>
        <div>
          <div className="section-kicker">Inbox, issue, agent, namespace</div>
          <h2>Every row has an operational consequence.</h2>
          <p>
            mspace keeps the everyday surface quiet, but the language stays concrete: issues,
            sessions, worktrees, branches, clusters, namespaces, preview URLs, and cleanup.
          </p>
        </div>
      </section>

      <section className="control-section" id="architecture">
        <div className="section-kicker">Control contract</div>
        <div className="control-grid">
          <div className="control-lead">
            <h2>Local development. Kubernetes validation. Server-owned collaboration.</h2>
            <p>
              The MVP is local-first, but the product boundary is already clear: the server owns
              users and workspace state, the runner owns execution, and Kubernetes is the
              validation target.
            </p>
          </div>
          <div className="control-points">
            {controlPoints.map((point) => {
              const Icon = point.icon;
              return (
                <article key={point.title}>
                  <Icon aria-hidden="true" size={22} strokeWidth={2} />
                  <h3>{point.title}</h3>
                  <p>{point.body}</p>
                </article>
              );
            })}
          </div>
        </div>
        <div className="architecture-map" aria-label="mspace runtime topology">
          <div className="topology-node">
            <span>01</span>
            <strong>Desktop workspace</strong>
            <p>Inbox, issues, comments, agents, projects</p>
          </div>
          <div className="topology-arrow">claim</div>
          <div className="topology-node">
            <span>02</span>
            <strong>Go runner</strong>
            <p>SQLite state, HTTP API, SSE, worktree prep</p>
          </div>
          <div className="topology-arrow">turn</div>
          <div className="topology-node">
            <span>03</span>
            <strong>Codex app-server</strong>
            <p>Thread, turn, status, logs, source changes</p>
          </div>
          <div className="topology-node topology-wide">
            <span>04</span>
            <strong>Git worktree</strong>
            <p>Branch, commit, diff preview, changed files</p>
          </div>
          <div className="topology-arrow">deploy</div>
          <div className="topology-node topology-k8s">
            <span>05</span>
            <strong>Kubernetes test namespace</strong>
            <p>Preview URL, resources, events, logs, cleanup state</p>
          </div>
        </div>
      </section>

      <section className="quick-start" id="quick-start">
        <div className="quick-copy">
          <div className="section-kicker">Run the local MVP</div>
          <h2>Start with a real repository and a real issue.</h2>
          <p>
            The website is only useful if it points back to the working loop. Install dependencies,
            launch the desktop app, then create an issue and attach an agent session.
          </p>
        </div>
        <div className="terminal-panel" aria-label="Quick start commands">
          <div className="terminal-top">
            <span>mspace.local</span>
            <span>ready</span>
          </div>
          {quickStart.map((command) => (
            <code key={command}>
              <span>$</span>
              {command}
            </code>
          ))}
        </div>
      </section>
    </main>
  );
}
