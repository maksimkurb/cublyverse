import { useState } from "react";

import type { Connection } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost } from "../../host/HostContext";
import { Button, Notice } from "../../ui/controls";
import { useConnectionsQuery, useRevokeConnection } from "./connections";
import { formatTokenDate } from "./tokens";

/**
 * «Подключённые приложения»: MCP-клиенты, которым пользователь разрешил доступ
 * через OAuth. Отключение — двумя нажатиями, как отзыв токена. Раздел скрыт,
 * пока подключений нет, чтобы не пугать пустым списком тех, кто пользуется
 * только токенами.
 */
export function ConnectionsSection() {
  const host = useHost();
  const query = useConnectionsQuery();
  const revoke = useRevokeConnection();
  const [armed, setArmed] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);

  async function handleRevoke(connection: Connection) {
    if (armed !== connection.id) {
      host.haptic("selection");
      setArmed(connection.id);
      return;
    }
    setRevoking(connection.id);
    try {
      await revoke.mutateAsync({ params: { path: { id: connection.id } } });
      host.haptic("success");
    } catch (cause) {
      host.haptic("error");
      await host.showAlert(`Не удалось отключить приложение: ${describeApiError(cause)}`);
    } finally {
      setArmed(null);
      setRevoking(null);
    }
  }

  if (query.isError) {
    return (
      <Notice tone="error">
        <span>Не удалось загрузить подключённые приложения: {query.error?.message ?? "неизвестная ошибка"}</span>
        <Button size="s" onClick={() => void query.refetch()}>
          Повторить
        </Button>
      </Notice>
    );
  }
  const connections = query.data;
  if (!connections || connections.length === 0) {
    return null;
  }

  return (
    <section aria-label="Подключённые приложения">
      <h2 className="page__subtitle">Подключённые приложения</h2>
      <ul className="card-list">
        {connections.map((connection) => (
          <li key={connection.id} className="card token">
            <div className="token__head">
              <span className="token__name">{connection.client_name}</span>
            </div>
            <ul className="scope-chips" aria-label="Области доступа">
              {connection.scopes.map((scope) => (
                <li className="scope-chip" key={scope}>
                  {scope}
                </li>
              ))}
            </ul>
            <span className="token__meta">
              Подключено: {formatTokenDate(connection.created_at)} · Использовано:{" "}
              {formatTokenDate(connection.last_used_at)}
            </span>
            <Button
              size="s"
              variant="danger"
              className="token__revoke"
              loading={revoking === connection.id}
              onClick={() => void handleRevoke(connection)}
            >
              {armed === connection.id ? "Подтвердить отключение" : "Отключить"}
            </Button>
          </li>
        ))}
      </ul>
    </section>
  );
}
