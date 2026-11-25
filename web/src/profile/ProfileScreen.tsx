import { useState } from "react";

import { api, type Me } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import TokensSection from "../features/tokens/TokensSection";
import { usePageTitle } from "../router/router";
import { Button, Notice, SelectField } from "../ui/controls";
import { timezoneOptions } from "./timezones";

/** Profile screen: user info, time zone selector and browser logout. */
export default function ProfileScreen() {
  const { user, host, updateUser, signOut } = useAuth();
  usePageTitle("Профиль");

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const zones = timezoneOptions(user.timezone);

  async function changeTimezone(timezone: string) {
    if (timezone === user.timezone) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const { data, error: apiError, response } = await api.PATCH("/me", { body: { timezone } });
      if (data) {
        updateUser(data);
      } else {
        setError(apiError?.message ?? `Сервер ответил ошибкой ${response.status}`);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(false);
    }
  }

  async function logout() {
    setBusy(true);
    setError(null);
    try {
      await signOut();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      setBusy(false);
    }
  }

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Профиль</h1>
      </header>

      <dl className="card facts">
        <div className="facts__row">
          <dt>Имя</dt>
          <dd>{user.display_name?.trim() || "не указано"}</dd>
        </div>
        <div className="facts__row">
          <dt>E-mail</dt>
          <dd>{user.email?.trim() || "не указан"}</dd>
        </div>
      </dl>

      <SelectField
        label="Часовой пояс"
        hint="В этом поясе считаются новые напоминания."
        value={user.timezone}
        disabled={busy}
        onChange={(event) => void changeTimezone(event.target.value)}
      >
        {zones.map((zone) => (
          <option value={zone} key={zone}>
            {zone}
          </option>
        ))}
      </SelectField>

      {error && <Notice tone="error">{error}</Notice>}

      <section className="page__section">
        <h2 className="page__heading">Привязанные аккаунты</h2>
        {user.bindings.length === 0 && <p className="page__state">Мессенджеры не привязаны.</p>}
        <ul className="card-list">
          {user.bindings.map((binding) => (
            <li className="card binding" key={`${binding.platform}:${binding.remote_user_id}`}>
              <span className="binding__platform">{platformLabel(binding.platform)}</span>
              <span className="cv-mono">{binding.remote_user_id}</span>
              <span className="binding__since">с {formatDate(binding.verified_at)}</span>
            </li>
          ))}
        </ul>
      </section>

      <TokensSection />

      {!host.isTelegram && (
        <Button block loading={busy} onClick={() => void logout()}>
          Выйти
        </Button>
      )}
    </div>
  );
}

function platformLabel(platform: Me["bindings"][number]["platform"]): string {
  switch (platform) {
    case "telegram":
      return "Telegram";
    case "matrix":
      return "Matrix";
    default:
      return platform;
  }
}

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleDateString("ru-RU");
}
