import { useState } from "react";
import { Button, Cell, List, Placeholder } from "@telegram-apps/telegram-ui";

import type { APIToken, CreatedToken } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { navigate, usePageTitle } from "../../router/router";
import { TokenCreateDialog } from "./TokenCreateDialog";
import { TokenRevealDialog } from "./TokenRevealDialog";
import { useRevokeToken, useTokensQuery } from "./data";
import { expiryLabel, formatTokenDate, prefixLabel, scopesLabel, tokenState, tokenStateLabel } from "./tokens";

/**
 * «Доступ по токенам»: the personal access tokens used by MCP clients and
 * scripts. The screen lists them, creates new ones and revokes them with a
 * second confirming tap. The plaintext appears only in the reveal dialog right
 * after creation.
 */
export default function TokensScreen() {
  const host = useHost();
  usePageTitle("Доступ по токенам");
  useHostBackButton(() => navigate("/"));

  const query = useTokensQuery();
  const revoke = useRevokeToken();

  const [createOpen, setCreateOpen] = useState(false);
  const [revealed, setRevealed] = useState<CreatedToken | null>(null);
  const [armed, setArmed] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);

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
    <main className="screen">
      <button type="button" className="screen__back" onClick={() => navigate("/")}>
        ← Главная
      </button>
      <h1 className="screen__title">Доступ по токенам</h1>
      <p className="screen__subtitle">
        Токены нужны внешним клиентам: MCP-серверу, скриптам, автоматизации. Они работают от вашего имени и
        ограничены выбранными областями доступа.
      </p>

      {query.isPending && (
        <p className="screen__state" role="status">
          Загрузка…
        </p>
      )}

      {query.isError && (
        <div className="screen__error" role="alert">
          <p>Не удалось загрузить токены: {query.error?.message ?? "неизвестная ошибка"}</p>
          <Button size="s" onClick={() => void query.refetch()}>
            Повторить
          </Button>
        </div>
      )}

      {query.isSuccess && tokens && tokens.length === 0 && (
        <Placeholder
          header="Нет токенов"
          description="Создайте токен, чтобы подключить Claude Code или другой MCP-клиент"
        />
      )}

      {tokens && tokens.length > 0 && (
        <List className="token-list">
          {tokens.map((token) => {
            const state = tokenState(token);
            const active = state === "active";
            return (
              <Cell key={token.id} className={active ? "token-row" : "token-row token-row--inactive"} multiline>
                <div className="token-row__content">
                  <div className="token-row__head">
                    <span className="token-row__name">{token.name}</span>
                    <span className={active ? "token-badge" : "token-badge token-badge--inactive"}>
                      {tokenStateLabel(state)}
                    </span>
                  </div>
                  <code className="token-row__prefix">{prefixLabel(token.prefix)}</code>
                  <span className="token-row__scopes">{scopesLabel(token.scopes)}</span>
                  <span className="token-row__meta">
                    Создан: {formatTokenDate(token.created_at)} · Использован: {formatTokenDate(token.last_used_at)} ·{" "}
                    {expiryLabel(token)}
                  </span>
                  {active && (
                    <Button
                      size="s"
                      mode="plain"
                      className={armed === token.id ? "token-row__revoke token-row__revoke--armed" : "token-row__revoke"}
                      loading={revoking === token.id}
                      disabled={revoking === token.id}
                      onClick={() => void handleRevoke(token)}
                    >
                      {armed === token.id ? "Подтвердить отзыв" : "Отозвать"}
                    </Button>
                  )}
                </div>
              </Cell>
            );
          })}
        </List>
      )}

      <Button className="screen__primary-action" size="l" stretched onClick={() => setCreateOpen(true)}>
        ＋ Создать токен
      </Button>

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
    </main>
  );
}
