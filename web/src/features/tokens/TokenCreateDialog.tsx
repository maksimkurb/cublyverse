import { useState, type FormEvent } from "react";

import type { APITokenScope, CreatedToken } from "../../api/client";
import { describeApiError } from "../../api/errors";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, Checkbox, Notice, SelectField, Sheet, TextField } from "../../ui/controls";
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
    <Sheet title="Новый токен" onClose={onClose}>
      <form className="cv-form" onSubmit={handleSubmit} noValidate>
        {error && (
          <Notice tone="error" title="Проверьте данные">
            {error}
          </Notice>
        )}

        <TextField
          label="Название"
          placeholder="Например: Claude Code"
          maxLength={80}
          value={values.name}
          onChange={(event) => setValues((current) => ({ ...current, name: event.target.value }))}
        />

        <fieldset className="scopes">
          <legend>Области доступа</legend>
          {SCOPE_OPTIONS.map((option) => (
            <label key={option.value} className="scopes__item">
              <Checkbox
                checked={values.scopes.includes(option.value)}
                onCheckedChange={(checked) => toggleScope(option.value, checked)}
              />
              <span className="scopes__text">
                <span className="scopes__label">{option.label}</span>
                <span className="scopes__description">{option.description}</span>
              </span>
            </label>
          ))}
        </fieldset>

        <SelectField
          label="Срок действия"
          hint="Токен показывается один раз. Храните его как пароль и отзывайте, если он попал не туда."
          value={values.expiry}
          onChange={(event) => setValues((current) => ({ ...current, expiry: event.target.value }))}
        >
          {EXPIRY_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </SelectField>

        <Button type="submit" variant="primary" block loading={busy}>
          Создать токен
        </Button>
      </form>
    </Sheet>
  );
}
