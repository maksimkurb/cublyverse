import { useState, type FormEvent } from "react";
import { Banner, Button, Checkbox, IconButton, Input, Modal, Select } from "@telegram-apps/telegram-ui";

import type { APITokenScope, CreatedToken } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { useCreateToken } from "./data";
import { EXPIRY_OPTIONS, SCOPE_OPTIONS, buildCreateTokenRequest, emptyTokenForm } from "./tokens";

/** Props of the create dialog. */
export interface TokenCreateDialogProps {
  onClose: () => void;
  /** Called with the created token so the reveal dialog can show the secret. */
  onCreated: (created: CreatedToken) => void;
}

/**
 * Create dialog: name, scopes and lifetime. The secret is shown by a separate
 * reveal dialog exactly once; nothing here keeps it after the request.
 */
export function TokenCreateDialog({ onClose, onCreated }: TokenCreateDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const create = useCreateToken();

  const [values, setValues] = useState(emptyTokenForm);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  function toggleScope(scope: APITokenScope, checked: boolean) {
    host.haptic("selection");
    setValues((current) => ({
      ...current,
      scopes: checked
        ? [...current.scopes.filter((item) => item !== scope), scope]
        : current.scopes.filter((item) => item !== scope),
    }));
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = buildCreateTokenRequest(values);
    if (!result.ok) {
      setError(result.message);
      host.haptic("error");
      return;
    }
    setError(null);
    setBusy(true);
    try {
      const created = await create.mutateAsync({ body: result.body });
      host.haptic("success");
      onCreated(created);
    } catch (cause) {
      setError(describeApiError(cause));
      host.haptic("error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      open
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
      header={
        <div className="token-editor__header">
          <Modal.Close>
            <IconButton aria-label="Закрыть" size="s" mode="plain">
              ×
            </IconButton>
          </Modal.Close>
          <h2 className="token-editor__title">Новый токен</h2>
          <span className="token-editor__spacer" aria-hidden="true" />
        </div>
      }
    >
      <form className="token-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <div role="alert" className="token-form__error">
            <Banner type="inline" header="Проверьте данные" description={error} />
          </div>
        )}

        <Input
          header="Название"
          aria-label="Название"
          placeholder="Например: Claude Code"
          maxLength={80}
          value={values.name}
          onChange={(event) => setValues((current) => ({ ...current, name: event.target.value }))}
        />

        <fieldset className="token-form__scopes">
          <legend>Области доступа</legend>
          {SCOPE_OPTIONS.map((option) => (
            <label key={option.value} className="token-form__scope">
              <Checkbox
                checked={values.scopes.includes(option.value)}
                onChange={(event) => toggleScope(option.value, event.target.checked)}
              />
              <span className="token-form__scope-text">
                <span className="token-form__scope-label">{option.label}</span>
                <span className="token-form__scope-description">{option.description}</span>
              </span>
            </label>
          ))}
        </fieldset>

        <Select
          header="Срок действия"
          aria-label="Срок действия"
          value={values.expiry}
          onChange={(event) => setValues((current) => ({ ...current, expiry: event.target.value }))}
        >
          {EXPIRY_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </Select>

        <p className="token-form__note">
          Токен показывается один раз. Храните его как пароль и отзывайте, если он попал не туда.
        </p>

        <Button type="submit" size="l" stretched loading={busy} disabled={busy}>
          Создать токен
        </Button>
      </form>
    </Modal>
  );
}
