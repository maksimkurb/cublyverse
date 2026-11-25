import type { components } from "../../api/schema";

/** Mini-app that keeps its data in spaces. */
export type SpaceApp = components["schemas"]["SpaceApp"];

/** Wording of the space screens of every mini-app. */
export const SPACE_APPS: Record<SpaceApp, { onboarding: string; sharing: string }> = {
  warehouse: {
    onboarding: "Склад хранит запасы, закупки, проекты и каталог. Начните со своего личного склада: людей можно пригласить позже.",
    sharing: "Все участники равны: любой может менять склад и состав пространства. Напоминания это не затрагивает.",
  },
  reminders: {
    onboarding: "Напоминания живут в пространстве. Начните с личного, а для общих дел позже создайте отдельное и пригласите людей.",
    sharing: "Все участники видят и меняют напоминания этого пространства. Кому присылать, выбирается в каждом напоминании.",
  },
};
