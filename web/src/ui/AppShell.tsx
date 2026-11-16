import type { ComponentType, ReactNode } from "react";

import { features } from "../features/registry";
import { Link } from "../router/router";
import { IconUser, Logo } from "./icons";

interface NavItem {
  route: string;
  title: string;
  icon: ComponentType<{ size?: number }>;
}

/** Sections of the app: every registered feature, then the profile. */
export const navItems: NavItem[] = [
  ...features.map((feature) => ({ route: feature.route, title: feature.title, icon: feature.icon })),
  { route: "/profile", title: "Профиль", icon: IconUser },
];

/** The route a bare `/app/` opens: the first feature. */
export const homeRoute = navItems[0].route;

/**
 * Frame of every signed-in screen: the brand, the section navigation (a bottom
 * bar on a phone, a sidebar on a wide screen) and the screen itself.
 */
export function AppShell({ route, children }: { route: string; children: ReactNode }) {
  const current = route === "/" ? homeRoute : route;

  return (
    <div className="cv-shell">
      <header className="cv-shell__brand">
        <Logo />
        <span className="cv-wordmark">cublyverse</span>
      </header>

      <nav className="cv-nav" aria-label="Разделы">
        {navItems.map((item) => {
          const Icon = item.icon;
          const active = item.route === current;
          return (
            <Link
              key={item.route}
              to={item.route}
              className="cv-nav__item"
              aria-current={active ? "page" : undefined}
            >
              <span className="cv-nav__icon">
                <Icon size={16} />
              </span>
              {item.title}
            </Link>
          );
        })}
      </nav>

      <main className="cv-shell__main">{children}</main>
    </div>
  );
}
