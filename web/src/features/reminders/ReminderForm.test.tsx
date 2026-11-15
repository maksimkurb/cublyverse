import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { makeReminder } from "../../test/fixtures";
import { renderWithHost } from "../../test/render";
import { ReminderForm } from "./ReminderForm";
import { emptyFormValues, formValuesFromReminder, type SavePlan } from "./form";

function renderForm(
  overrides: Partial<Parameters<typeof ReminderForm>[0]> = {},
) {
  const onSave = vi.fn(async (plan: SavePlan) => {
    void plan;
  });
  const props = {
    mode: "create" as const,
    initial: emptyFormValues(),
    timezone: "Europe/Moscow",
    onSave,
    ...overrides,
  };
  const view = renderWithHost(<ReminderForm {...props} />);
  return { ...view, onSave };
}

describe("ReminderForm", () => {
  it("shows a validation message instead of saving an empty message", async () => {
    const { onSave } = renderForm();
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Сообщение должно быть от 1 до 1500 символов",
    );
    expect(onSave).not.toHaveBeenCalled();
  });

  it("validates the repeat interval", async () => {
    renderForm();
    fireEvent.change(screen.getByLabelText("Что напомнить"), { target: { value: "Пить воду" } });
    fireEvent.change(screen.getByLabelText("Повторять, минут"), { target: { value: "0" } });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Интервал повтора должен быть от 1 до 1440 минут",
    );
  });

  it("maps the form into one request per time", async () => {
    const { onSave } = renderForm();

    fireEvent.change(screen.getByLabelText("Что напомнить"), { target: { value: "  Пить воду  " } });
    fireEvent.change(screen.getByLabelText("Время"), { target: { value: "08:15" } });
    fireEvent.click(screen.getByRole("button", { name: "+ Добавить время" }));
    fireEvent.change(screen.getAllByLabelText("Ещё время")[0], { target: { value: "20:45" } });
    fireEvent.change(screen.getByLabelText("Повторять, минут"), { target: { value: "30" } });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    const plan = onSave.mock.calls[0][0] as SavePlan;
    expect(plan.primary).toEqual({
      message: "Пить воду",
      timezone: "Europe/Moscow",
      schedule_kind: "weekly",
      schedule_days: [1, 2, 3, 4, 5, 6, 7],
      time: "08:15",
      repeat_minutes: 30,
      enabled: true,
    });
    expect(plan.extra).toHaveLength(1);
    expect(plan.extra[0].time).toBe("20:45");
  });

  it("can deselect a weekday and disable the reminder", async () => {
    const { onSave } = renderForm();
    fireEvent.change(screen.getByLabelText("Что напомнить"), { target: { value: "Полить цветы" } });
    fireEvent.click(screen.getByRole("button", { name: "Ср" }));
    fireEvent.click(screen.getByLabelText("Включено"));
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    const plan = onSave.mock.calls[0][0] as SavePlan;
    expect(plan.primary.schedule_days).toEqual([1, 2, 4, 5, 6, 7]);
    expect(plan.primary.enabled).toBe(false);
  });

  it("switches to monthly days and validates them", async () => {
    const { onSave } = renderForm();
    fireEvent.change(screen.getByLabelText("Что напомнить"), { target: { value: "Отчёт" } });
    fireEvent.change(screen.getByLabelText("Расписание"), { target: { value: "monthly" } });
    fireEvent.change(screen.getByLabelText("Дни месяца"), { target: { value: "1, 15, 40" } });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Дни месяца должны быть от 1 до 31");
    expect(onSave).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Дни месяца"), { target: { value: "1, 15, 30" } });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));
    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    const plan = onSave.mock.calls[0][0] as SavePlan;
    expect(plan.primary.schedule_kind).toBe("monthly");
    expect(plan.primary.schedule_days).toEqual([1, 15, 30]);
  });

  it("deletes only after the second tap", async () => {
    const onDelete = vi.fn(async () => {});
    renderForm({
      mode: "edit",
      initial: formValuesFromReminder(makeReminder()),
      onDelete,
    });

    fireEvent.click(screen.getByRole("button", { name: "Удалить напоминание" }));
    expect(onDelete).not.toHaveBeenCalled();
    const armed = screen.getByRole("button", { name: "Подтвердить удаление" });

    fireEvent.click(armed);
    await waitFor(() => expect(onDelete).toHaveBeenCalledTimes(1));
  });

  it("keeps the reminder time zone when editing", async () => {
    const { onSave } = renderForm({
      mode: "edit",
      initial: formValuesFromReminder(makeReminder({ timezone: "Asia/Oral" })),
      timezone: "Asia/Oral",
    });
    fireEvent.click(screen.getByRole("button", { name: "Сохранить" }));

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1));
    const plan = onSave.mock.calls[0][0] as SavePlan;
    expect(plan.primary.timezone).toBe("Asia/Oral");
    expect(plan.primary.time).toBe("09:30");
    expect(plan.extra).toHaveLength(0);
  });
});
