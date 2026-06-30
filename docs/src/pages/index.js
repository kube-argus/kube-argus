import React from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import styles from './index.module.css';

const components = [
  {
    label: '01 / operator',
    title: 'Operator',
    body: 'Reconciles UserAuthenticationBind CRs into a per-user ServiceAccount plus the matching Role and ClusterRole bindings. Membership changes re-sync; binds expire on TTL.',
  },
  {
    label: '02 / broker',
    title: 'Broker',
    body: 'OIDC provider to your client, Relying Party to your IdP. On login it writes the CR, waits for the bind, and mints a ServiceAccount token.',
  },
  {
    label: '03 / proxy',
    title: 'Proxy',
    body: 'Front-door that injects the minted token, so existing clients reach the cluster without knowing about the bind flow.',
  },
];

const snippet = `apiVersion: rbac.kargus.io/v1
kind: UserAuthenticationBind
metadata:
  name: alice
spec:
  subject: alice@corp.example
  memberships:
    - platform-admins
    - sre
  ttl: 8h`;

function sourceHref(siteConfig) {
  return siteConfig.themeConfig.navbar.items.find((i) => i.label === 'Source')?.href;
}

function Hero() {
  const { siteConfig } = useDocusaurusContext();
  return (
    <header className={styles.hero}>
      <div className={styles.heroInner}>
        <div className={styles.heroCopy}>
          <p className={styles.eyebrow}>Kubernetes RBAC · driven by your IdP</p>
          <h1 className={styles.heroTitle}>{siteConfig.title}</h1>
          <p className={styles.heroTagline}>{siteConfig.tagline}</p>
          <div className={styles.heroButtons}>
            <Link className={styles.btnPrimary} to="/docs">
              Read the docs →
            </Link>
            <Link className={styles.btnGhost} href={sourceHref(siteConfig)}>
              Source
            </Link>
          </div>
        </div>
        <div className={styles.terminal}>
          <div className={styles.terminalBar}>
            <span />
            <span className={styles.terminalName}>userauthenticationbind.yaml</span>
          </div>
          <pre className={styles.terminalBody}>{snippet}</pre>
        </div>
      </div>
    </header>
  );
}

export default function Home() {
  const { siteConfig } = useDocusaurusContext();
  return (
    <Layout title={siteConfig.title} description={siteConfig.tagline}>
      <Hero />
      <main>
        <section className={styles.section}>
          <p className={styles.eyebrow}>Three components</p>
          <div className={styles.cards}>
            {components.map((c) => (
              <div key={c.title} className={styles.card}>
                <span className={styles.cardLabel}>{c.label}</span>
                <h3 className={styles.cardTitle}>{c.title}</h3>
                <p className={styles.cardBody}>{c.body}</p>
              </div>
            ))}
          </div>
        </section>
      </main>
    </Layout>
  );
}
