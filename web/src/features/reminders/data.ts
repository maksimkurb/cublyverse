import { useQueryClient } from "@tanstack/react-query";

import type { ApiError, CreateReminderRequest, Reminder, UpdateReminderRequest } from "../../api/client";
import { $api } from "../../api/query";

/** Query key of `GET /reminders`, as built by openapi-react-query. */
export const remindersQueryKey = ["get", "/reminders"] as const;

/** Loads the reminders of one space. */
export function useRemindersQuery(spaceId: string) {
  return $api.useQuery("get", "/reminders", { params: { query: { space_id: spaceId } } }, { enabled: spaceId !== "" });
}

/** Creates one reminder. */
export function useCreateReminder() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/reminders", {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: remindersQueryKey }),
  });
}

/** Replaces one reminder. */
export function useUpdateReminder() {
  const queryClient = useQueryClient();
  return $api.useMutation("put", "/reminders/{id}", {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: remindersQueryKey }),
  });
}

/** Deletes one reminder. */
export function useDeleteReminder() {
  const queryClient = useQueryClient();
  return $api.useMutation("delete", "/reminders/{id}", {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: remindersQueryKey }),
  });
}

/** Confirms the pending occurrence, like «Выполнено» in the chat. */
export function useCompleteReminder() {
  const queryClient = useQueryClient();
  return $api.useMutation("post", "/reminders/{id}/complete", {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: remindersQueryKey }),
  });
}

/** Body sent by the toggle: the whole rule, because `PUT` replaces it. */
export function toggleBody(reminder: Reminder, enabled: boolean): UpdateReminderRequest {
  return {
    message: reminder.message,
    timezone: reminder.timezone,
    schedule_kind: reminder.schedule_kind,
    schedule_days: reminder.schedule_days,
    interval: reminder.interval,
    month_week_position: reminder.month_week_position,
    month_weekday: reminder.month_weekday,
    start_date: reminder.start_date,
    end_kind: reminder.end_kind,
    end_date: reminder.end_date,
    end_count: reminder.end_count,
    time: reminder.time,
    repeat_minutes: reminder.repeat_minutes,
    enabled,
  };
}

/** Error type of every generated operation. */
export type ReminderApiError = ApiError;

/** Request body of one create call. */
export type ReminderCreateBody = CreateReminderRequest;
