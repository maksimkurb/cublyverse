import type {
  APIToken,
  Connection,
  CraftCategory,
  CraftItem,
  CraftStockEntry,
  Project,
  ProjectMaterial,
  Reminder,
  Wish,
  WishLink,
  Workspace,
  WorkspaceCandidate,
  WorkspaceMember,
} from "../api/client";

/** Builds a reminder with sane defaults for tests. */
export function makeReminder(overrides: Partial<Reminder> = {}): Reminder {
  return {
    id: "11111111-1111-4111-8111-111111111111",
    space_id: "44444444-4444-4444-8444-444444444444",
    author_id: "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c",
    recipient_ids: ["6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c"],
    message: "Выпить таблетку",
    timezone: "Europe/Moscow",
    schedule_kind: "weekly",
    schedule_days: [1, 3, 5],
    interval: 1,
    month_week_position: null,
    month_weekday: null,
    start_date: "2026-01-05",
    end_kind: "none",
    end_date: null,
    end_count: null,
    occurrences_fired: 0,
    finished: false,
    finished_at: null,
    time: "09:30",
    repeat_minutes: 10,
    enabled: true,
    next_due_at: "2026-01-12T06:30:00Z",
    last_error: null,
    created_at: "2026-01-05T09:00:00Z",
    updated_at: "2026-01-05T09:00:00Z",
    ...overrides,
  };
}

/** Builds a personal access token with sane defaults for tests. */
export function makeToken(overrides: Partial<APIToken> = {}): APIToken {
  return {
    id: "22222222-2222-4222-8222-222222222222",
    name: "Claude Code",
    prefix: "9xQ2mB7k",
    scopes: ["profile:read", "reminders:read"],
    expires_at: "2027-04-15T09:30:00Z",
    last_used_at: "2026-01-16T08:00:00Z",
    revoked_at: null,
    created_at: "2026-01-15T09:30:00Z",
    ...overrides,
  };
}

/** Builds a connected OAuth application for tests. */
export function makeConnection(overrides: Partial<Connection> = {}): Connection {
  return {
    id: "33333333-3333-4333-8333-333333333333",
    client_name: "Claude",
    scopes: ["craft:read", "craft:write"],
    created_at: "2026-01-15T09:30:00Z",
    last_used_at: "2026-01-16T08:00:00Z",
    ...overrides,
  };
}

/** Builds a workspace with sane defaults for tests. */
export function makeWorkspace(overrides: Partial<Workspace> = {}): Workspace {
  return {
    id: "44444444-4444-4444-8444-444444444444",
    name: "Дом",
    app: "warehouse",
    personal: true,
    created_at: "2026-01-15T09:30:00Z",
    ...overrides,
  };
}

/** Builds a workspace member with sane defaults for tests. */
export function makeMember(overrides: Partial<WorkspaceMember> = {}): WorkspaceMember {
  return {
    user_id: "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c",
    display_name: "Иван Петров",
    email: "ivan@example.com",
    avatar_url: "https://www.gravatar.com/avatar/abc?s=96&d=identicon",
    joined_at: "2026-01-15T09:30:00Z",
    ...overrides,
  };
}

/** Builds a user who can be added to a workspace. */
export function makeCandidate(overrides: Partial<WorkspaceCandidate> = {}): WorkspaceCandidate {
  return {
    user_id: "77777777-7777-4777-8777-777777777777",
    display_name: "Анна Смирнова",
    email: "anna@example.com",
    avatar_url: "https://www.gravatar.com/avatar/def?s=96&d=identicon",
    ...overrides,
  };
}

/** Builds a craft category with sane defaults for tests. */
export function makeCategory(overrides: Partial<CraftCategory> = {}): CraftCategory {
  return {
    id: "8a3b1c2d-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
    slug: "pearls",
    name: "Жемчуг",
    default_unit: "шт",
    fields: [],
    catalog_slug: "",
    own_items: 0,
    ...overrides,
  };
}

/** Builds a catalogue item with sane defaults for tests. */
export function makeItem(overrides: Partial<CraftItem> = {}): CraftItem {
  return {
    id: "7d2e5c1a-8b3f-4c6d-9e0a-1b2c3d4e5f60",
    workspace_id: "44444444-4444-4444-8444-444444444444",
    private: true,
    category: "pearls",
    category_name: "Жемчуг",
    manufacturer: "PRECIOSA",
    article: "111 39 001",
    series: "",
    name: "Жемчуг айвори 2 мм",
    color_name: "айвори",
    color_hex: "#F2E8D5",
    color_source: "approx",
    size: "2 мм",
    finish: "",
    unit: "шт",
    pack_size: null,
    attrs: {},
    source: "manual",
    source_url: "",
    image_url: "",
    stock_qty: 7,
    in_inventory: true,
    ...overrides,
  };
}

/** Builds one stock row with sane defaults for tests. */
export function makeStock(overrides: Partial<CraftStockEntry> = {}): CraftStockEntry {
  return {
    item: makeItem(),
    qty: 7,
    location: "Ящик 2",
    notes: "Куплено на ярмарке",
    updated_at: "2026-01-16T08:00:00Z",
    ...overrides,
  };
}

/** Builds a shop link of a wish with sane defaults for tests. */
export function makeWishLink(overrides: Partial<WishLink> = {}): WishLink {
  return {
    id: "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d",
    url: "https://shop.example/beads",
    shop_name: "Магазин бусин",
    price: 12.5,
    currency: "RUB",
    in_stock: true,
    checked_at: null,
    created_at: "2026-01-15T09:30:00Z",
    ...overrides,
  };
}

/** Builds a wish list entry with sane defaults for tests. */
export function makeWish(overrides: Partial<Wish> = {}): Wish {
  return {
    id: "3f0b9a2c-6d41-4e7b-8c55-1a2b3c4d5e6f",
    catalog_item_id: makeItem().id,
    title: "Жемчуг айвори 2 мм",
    qty_wanted: 3,
    priority: 2,
    note: "на подарок",
    status: "wanted",
    created_by: "6f1c0a3e-5b9d-4a1f-9c2b-7e8d0f1a2b3c",
    created_at: "2026-01-15T09:30:00Z",
    updated_at: "2026-01-16T08:00:00Z",
    item: makeItem(),
    links: [makeWishLink()],
    ...overrides,
  };
}

/** Builds one planned material of a project for tests. */
export function makeMaterial(overrides: Partial<ProjectMaterial> = {}): ProjectMaterial {
  return {
    item: makeItem(),
    qty_planned: 20,
    qty_used: 5,
    shortage: 8,
    ...overrides,
  };
}

/** Builds a craft project with sane defaults for tests. */
export function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: "5c4d3e2f-1a0b-4c9d-8e7f-6a5b4c3d2e1f",
    name: "Серьги «Майорка»",
    notes: "Подарок на день рождения",
    created_at: "2026-01-15T09:30:00Z",
    material_count: 1,
    materials: [makeMaterial()],
    ...overrides,
  };
}
