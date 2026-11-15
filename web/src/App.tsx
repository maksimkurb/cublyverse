import { useEffect, useState } from "react";

import { fetchHealth, type Health } from "./api/client";

type State =
  | { status: "loading" }
  | { status: "ready"; health: Health }
  | { status: "error"; message: string };

/**
 * Placeholder screen for M0: it proves that the WebApp talks to the API and
 * shows the version reported by the running backend.
 */
export default function App() {
  const [state, setState] = useState<State>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;

    fetchHealth()
      .then((health) => {
        if (!cancelled) {
          setState({ status: "ready", health });
        }
      })
      .catch((cause: unknown) => {
        if (!cancelled) {
          setState({
            status: "error",
            message: cause instanceof Error ? cause.message : String(cause),
          });
        }
      });

    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <main className="app">
      <h1 className="app__title">Cublyverse</h1>
      <p className="app__subtitle">Персональный бот-ассистент</p>

      {state.status === "loading" && (
        <p className="app__status" role="status">
          Загрузка…
        </p>
      )}

      {state.status === "error" && (
        <p className="app__status app__status--error" role="alert">
          Бэкенд недоступен: {state.message}
        </p>
      )}

      {state.status === "ready" && (
        <section className="app__card">
          <p className="app__status" role="status">
            Бэкенд на связи: {state.health.status}
          </p>
          <p>
            Версия:{" "}
            <strong data-testid="version">{state.health.version}</strong>
          </p>
        </section>
      )}

      <footer className="app__footer">
        <a href="/api/docs">Документация API</a>
        <a href="/api/openapi.yaml">Спецификация</a>
      </footer>
    </main>
  );
}
