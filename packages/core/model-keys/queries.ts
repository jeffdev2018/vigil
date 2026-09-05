import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const modelKeyKeys = {
  all: (wsId: string) => ["model-keys", wsId] as const,
};

export function modelKeysOptions(wsId: string) {
  return queryOptions({ queryKey: modelKeyKeys.all(wsId), queryFn: () => api.listModelKeys(wsId), enabled: !!wsId });
}
