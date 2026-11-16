import type { ReactNode } from "react";

import { Button } from "../ui/controls";
import { Logo } from "../ui/icons";

/** The dark full-screen frame of every page shown before the app opens. */
function Gate({ title, children, action }: { title?: string; children: ReactNode; action?: ReactNode }) {
  return (
    <main className="cv-gate">
      <div className="cv-gate__brand">
        <Logo />
        <span className="cv-wordmark">cublyverse</span>
      </div>
      <div className="cv-gate__body">
        {title && <h1 className="cv-gate__title">{title}</h1>}
        <div className="cv-gate__text">{children}</div>
      </div>
      {action && <div className="cv-gate__actions">{action}</div>}
    </main>
  );
}

/** Shown while the session is being established. */
export function SignInProgress() {
  return (
    <Gate>
      <p role="status">Загрузка…</p>
    </Gate>
  );
}

/**
 * A plain browser without a session: the only way in is the OIDC flow below
 * `/auth/login`, which returns to the WebApp with a cookie session.
 */
export function LoginRequiredScreen() {
  return (
    <Gate
      title="Нужно войти"
      action={
        <a className="cv-btn cv-btn--primary cv-btn--block" href="/auth/login?return_to=/app/">
          Войти
        </a>
      }
    >
      <p>Войдите через SSO, чтобы открыть приложение.</p>
    </Gate>
  );
}

/** The Telegram account has no binding yet. */
export function LinkRequiredScreen({ linkUrl, onOpen }: { linkUrl: string; onOpen: (url: string) => void }) {
  return (
    <Gate
      title="Нужно привязать аккаунт"
      action={
        <Button variant="primary" block onClick={() => onOpen(linkUrl)}>
          Привязать аккаунт
        </Button>
      }
    >
      <p>
        Этот Telegram-аккаунт ещё не привязан к SSO. Привяжите его, чтобы напоминания приходили в этот чат.
      </p>
    </Gate>
  );
}

/** The session could not be established or refreshed. */
export function SessionErrorScreen({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <Gate
      title="Не удалось открыть приложение"
      action={
        <Button variant="primary" block onClick={onRetry}>
          Повторить
        </Button>
      }
    >
      <p role="alert">{message}</p>
    </Gate>
  );
}
