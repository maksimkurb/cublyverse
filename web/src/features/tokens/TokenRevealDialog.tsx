import { useState } from "react";
import { Banner, Button, IconButton, Modal } from "@telegram-apps/telegram-ui";

import type { CreatedToken } from "../../api/client";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { copyToken } from "./tokens";

/** Props of the one-time reveal dialog. */
export interface TokenRevealDialogProps {
  created: CreatedToken;
  onClose: () => void;
}

/**
 * One-time reveal: the only moment the plaintext exists outside the client. The
 * dialog says so explicitly and offers a copy button; closing it drops the
 * value, because the server cannot show it again.
 */
export function TokenRevealDialog({ created, onClose }: TokenRevealDialogProps) {
  useHostBackButton(onClose);
  const host = useHost();
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    const ok = await copyToken(created.token);
    setCopied(ok);
    host.haptic(ok ? "success" : "error");
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
          <h2 className="token-editor__title">Токен создан</h2>
          <span className="token-editor__spacer" aria-hidden="true" />
        </div>
      }
    >
      <div className="token-reveal">
        <Banner
          type="inline"
          header="Скопируйте токен сейчас"
          description="Больше он не будет показан: сервер хранит только его хеш. Если токен потерян, отзовите его и создайте новый."
        />

        <code className="token-reveal__value" aria-label="Значение токена">
          {created.token}
        </code>

        <Button size="l" stretched onClick={() => void handleCopy()}>
          {copied ? "Скопировано" : "Скопировать"}
        </Button>
        {!copied && (
          <p className="token-reveal__hint">
            Если браузер не даёт доступ к буферу обмена, выделите значение выше и скопируйте вручную.
          </p>
        )}

        <Button size="l" stretched mode="plain" onClick={onClose}>
          Готово
        </Button>
      </div>
    </Modal>
  );
}
