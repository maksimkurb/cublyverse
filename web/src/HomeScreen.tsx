import { Cell, List } from "@telegram-apps/telegram-ui";

import { useAuth } from "./auth/AuthProvider";
import { features } from "./features/registry";
import { navigate, usePageTitle } from "./router/router";

/** Home screen: the list of features plus the profile entry. */
export default function HomeScreen() {
  const { user } = useAuth();
  usePageTitle("Cublyverse");

  const name = user.display_name?.trim() || user.email?.trim();

  return (
    <main className="screen">
      <header className="home__header">
        <p className="home__eyebrow">Cublyverse</p>
        <h1 className="screen__title">{name ? `Привет, ${name}` : "Личный ассистент"}</h1>
        <p className="home__subtitle">Напоминания, бот и MCP в одном месте</p>
      </header>

      <List className="home__features">
        {features.map((feature) => (
          <Cell
            key={feature.id}
            before={<span className="home__icon">{feature.icon}</span>}
            after={<span className="home__chevron">›</span>}
            role="link"
            tabIndex={0}
            aria-label={feature.title}
            onClick={() => navigate(feature.route)}
            onKeyDown={(event) => {
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                navigate(feature.route);
              }
            }}
          >
            {feature.title}
          </Cell>
        ))}
        <Cell
          before={<span className="home__icon">👤</span>}
          after={<span className="home__chevron">›</span>}
          subtitle={user.timezone}
          role="link"
          tabIndex={0}
          aria-label="Профиль"
          onClick={() => navigate("/profile")}
          onKeyDown={(event) => {
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              navigate("/profile");
            }
          }}
        >
          Профиль
        </Cell>
      </List>
    </main>
  );
}
