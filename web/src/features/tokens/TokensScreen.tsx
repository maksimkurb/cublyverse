import { useState } from "react";

import type { APIToken, CreatedToken } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost } from "../../host/HostContext";
import { usePageTitle } from "../../router/router";
import { Button, EmptyState, Notice } from "../../ui/controls";
import { IconCopy, IconPlus } from "../../ui/icons";
import { ConnectionsSection } from "./ConnectionsSection";
import { TokenCreateDialog } from "./TokenCreateDialog";
import { TokenRevealDialog } from "./TokenRevealDialog";
import { useRevokeToken, useTokensQuery } from "./data";
import { copyToken, expiryLabel, formatTokenDate, prefixLabel, tokenState, tokenStateLabel } from "./tokens";

/**
 * «Токены»: the MCP endpoint and the personal access tokens used by MCP clients and
 * scripts. The screen lists them, creates new ones and revokes them with a
 * second confirming tap. The plaintext appears only in the reveal dialog right
 * after creation.
 */
export default function TokensScreen() {
  const host = useHost();
  usePageTitle("Токены");

  const query = useTokensQuery();
  const revoke = useRevokeToken();

  const [createOpen, setCreateOpen] = useState(false);
  const [revealed, setRevealed] = useState<CreatedToken | null>(null);
  const [armed, setArmed] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const mcpUrl = `${window.location.origin}/mcp`;

  async function copyMcpUrl() {
    const ok = await copyToken(mcpUrl);
    setCopied(ok);
    host.haptic(ok ? "success" : "error");
  }

  async function handleRevoke(token: APIToken) {
    if (armed !== token.id) {
      host.haptic("selection");
      setArmed(token.id);
      return;
    }
    setRevoking(token.id);
    try {
      await revoke.mutateAsync({ params: { path: { id: token.id } } });
      host.haptic("success");
    } catch (cause) {
      host.haptic("error");
      await host.showAlert(`Не удалось отозвать токен: ${describeApiError(cause)}`);
    } finally {
      setArmed(null);
      setRevoking(null);
    }
  }

  const tokens = query.data;

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Токены</h1>
        <Button variant="primary" aria-label="Новый токен" onClick={() => setCreateOpen(true)}>
          <IconPlus />
          Новый
        </Button>
      </header>

      <section className="mcp" aria-label="MCP-сервер">
        <div className="mcp__meta">
          <span className="cv-eyebrow">MCP-сервер</span>
          <span>Streamable HTTP</span>
        </div>
        <div className="mcp__row">
          <code className="cv-code mcp__url">{mcpUrl}</code>
          <button
            type="button"
            className="cv-iconbtn mcp__copy"
            aria-label={copied ? "Адрес MCP скопирован" : "Скопировать адрес MCP"}
            onClick={() => void copyMcpUrl()}
          >
            <IconCopy />
          </button>
        </div>
        <p className="mcp__text">
          Токены нужны внешним клиентам: MCP, скриптам, автоматизации. Они работают от вашего имени и ограничены
          выбранными областями доступа. Клиент передаёт токен в заголовке Authorization.
        </p>
      </section>

      {query.isPending && (
        <p className="page__state" role="status">
          Загрузка…
        </p>
      )}

      {query.isError && (
        <Notice tone="error">
          <span>Не удалось загрузить токены: {query.error?.message ?? "неизвестная ошибка"}</span>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </Notice>
      )}

      {query.isSuccess && tokens && tokens.length === 0 && (
        <EmptyState title="Нет токенов">Создайте токен, чтобы подключить Claude Code или другой MCP-клиент</EmptyState>
      )}

      {tokens && tokens.length > 0 && (
        <ul className="card-list">
          {tokens.map((token) => {
            const state = tokenState(token);
            const active = state === "active";
            return (
              <li key={token.id} className={active ? "card token" : "card token token--inactive"}>
                <div className="token__head">
                  <span className="token__name">{token.name}</span>
                  <span className={active ? "badge badge--on" : "badge"}>{tokenStateLabel(state)}</span>
                </div>
                <code className="cv-mono token__prefix">{prefixLabel(token.prefix)}</code>
                <ul className="scope-chips" aria-label="Области доступа">
                  {token.scopes.map((scope) => (
                    <li className="scope-chip" key={scope}>
                      {scope}
                    </li>
                  ))}
                </ul>
                <span className="token__meta">
                  Создан: {formatTokenDate(token.created_at)} · Использован: {formatTokenDate(token.last_used_at)} ·{" "}
                  {expiryLabel(token)}
                </span>
                {active && (
                  <Button
                    size="s"
                    variant="danger"
                    className="token__revoke"
                    loading={revoking === token.id}
                    onClick={() => void handleRevoke(token)}
                  >
                    {armed === token.id ? "Подтвердить отзыв" : "Отозвать"}
                  </Button>
                )}
              </li>
            );
          })}
        </ul>
      )}

      <ConnectionsSection />

      {createOpen && (
        <TokenCreateDialog
          onClose={() => setCreateOpen(false)}
          onCreated={(created) => {
            setCreateOpen(false);
            setRevealed(created);
          }}
        />
      )}

      {revealed && <TokenRevealDialog created={revealed} onClose={() => setRevealed(null)} />}
    </div>
  );
}
