import { useState } from "react";
import { Button, List, Select, Text } from "@telegram-apps/telegram-ui";

import { api, type Me } from "../api/client";
import { useAuth } from "../auth/AuthProvider";
import { useHostBackButton } from "../host/HostContext";
import { navigate, usePageTitle } from "../router/router";
import { timezoneOptions } from "./timezones";

/** Profile screen: user info, time zone selector and browser logout. */
export default function ProfileScreen() {
  const { user, host, updateUser, signOut } = useAuth();
  usePageTitle("Профиль");
  useHostBackButton(() => navigate("/"));

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
    <main className="screen">
      <button type="button" className="screen__back" onClick={() => navigate("/")}>
        ← Главная
      </button>
      <h1 className="screen__title">Профиль</h1>

      <List className="profile__card">
        <div className="profile__row">
          <span className="profile__label">Имя</span>
          <span>{user.display_name?.trim() || "не указано"}</span>
        </div>
        <div className="profile__row">
          <span className="profile__label">E-mail</span>
          <span>{user.email?.trim() || "не указан"}</span>
        </div>
        <div className="profile__row">
          <span className="profile__label">Часовой пояс</span>
          <span>{user.timezone}</span>
        </div>
      </List>

      <Select
        header="Часовой пояс"
        aria-label="Часовой пояс"
        value={user.timezone}
        disabled={busy}
        onChange={(event) => void changeTimezone(event.target.value)}
      >
        {zones.map((zone) => (
          <option value={zone} key={zone}>
            {zone}
          </option>
        ))}
      </Select>

      {error && (
        <p className="screen__error" role="alert">
          {error}
        </p>
      )}

      <section className="profile__bindings">
        <h2 className="profile__heading">Привязанные аккаунты</h2>
        {user.bindings.length === 0 && <Text>Мессенджеры не привязаны.</Text>}
        {user.bindings.map((binding) => (
          <div className="profile__binding" key={`${binding.platform}:${binding.remote_user_id}`}>
            <strong>{platformLabel(binding.platform)}</strong>
            <span>{binding.remote_user_id}</span>
            <span className="profile__muted">с {formatDate(binding.verified_at)}</span>
          </div>
        ))}
      </section>

      {!host.isTelegram && (
        <Button
          className="profile__logout"
          size="l"
          stretched
          mode="gray"
          loading={busy}
          disabled={busy}
          onClick={() => void logout()}
        >
          Выйти
        </Button>
      )}
    </main>
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
