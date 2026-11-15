import { Button, Placeholder, Spinner, Text } from "@telegram-apps/telegram-ui";

/** Shown while the session is being established. */
export function SignInProgress() {
  return (
    <main className="screen screen--center">
      <Spinner size="m" />
      <Text>Загрузка…</Text>
    </main>
  );
}

/**
 * A plain browser without a session: the only way in is the OIDC flow below
 * `/auth/login`, which returns to the WebApp with a cookie session.
 */
export function LoginRequiredScreen() {
  return (
    <main className="screen">
      <Placeholder
        header="Нужно войти"
        description="Войдите через SSO, чтобы открыть приложение."
        action={
          <a className="cv-button cv-button--primary" href="/auth/login?return_to=/app/">
            Войти
          </a>
        }
      />
    </main>
  );
}

/** The Telegram account has no binding yet. */
export function LinkRequiredScreen({ linkUrl, onOpen }: { linkUrl: string; onOpen: (url: string) => void }) {
  return (
    <main className="screen">
      <Placeholder
        header="Нужно привязать аккаунт"
        description="Этот Telegram-аккаунт ещё не привязан к SSO. Привяжите его, чтобы напоминания приходили в этот чат."
        action={
          <Button stretched onClick={() => onOpen(linkUrl)}>
            Привязать аккаунт
          </Button>
        }
      />
    </main>
  );
}

/** The session could not be established or refreshed. */
export function SessionErrorScreen({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <main className="screen">
      <Placeholder
        header="Не удалось открыть приложение"
        description={<span role="alert">{message}</span>}
        action={
          <Button stretched onClick={onRetry}>
            Повторить
          </Button>
        }
      />
    </main>
  );
}
