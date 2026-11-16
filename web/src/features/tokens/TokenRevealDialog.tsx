import { useState } from "react";

import type { CreatedToken } from "../../api/client";
import { useHost, useHostBackButton } from "../../host/HostContext";
import { Button, Sheet } from "../../ui/controls";
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
    <Sheet title="Токен создан" onClose={onClose}>
      <div className="cv-form">
        <div className="reveal">
          <strong>Скопируйте токен сейчас</strong>
          <span>
            Больше он не будет показан: сервер хранит только его хеш. Если токен потерян, отзовите его и создайте
            новый.
          </span>
          <code className="cv-code reveal__value" aria-label="Значение токена">
            {created.token}
          </code>
        </div>

        <Button variant="primary" block onClick={() => void handleCopy()}>
          {copied ? "Скопировано" : "Скопировать"}
        </Button>
        {!copied && (
          <p className="cv-field__hint">
            Если браузер не даёт доступ к буферу обмена, выделите значение выше и скопируйте вручную.
          </p>
        )}

        <Button block onClick={onClose}>
          Готово
        </Button>
      </div>
    </Sheet>
  );
}
