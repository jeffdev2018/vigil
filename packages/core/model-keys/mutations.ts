import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateModelKeyRequest } from "./schemas";
import { modelKeyKeys } from "./queries";

function useModelKeyMutation<T>(wsId: string, mutationFn: (value: T) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({ mutationFn, onSettled: () => qc.invalidateQueries({ queryKey: modelKeyKeys.all(wsId) }) });
}

export function useCreateModelKey(wsId: string) {
  return useModelKeyMutation(wsId, (data: CreateModelKeyRequest) => api.createModelKey(wsId, data));
}

export function useRotateModelKey(wsId: string) {
  return useModelKeyMutation(wsId, ({ keyId, key, label }: { keyId: string; key: string; label?: string }) => api.rotateModelKey(wsId, keyId, key, label));
}

export function useRetireModelKey(wsId: string) {
  return useModelKeyMutation(wsId, (keyId: string) => api.retireModelKey(wsId, keyId));
}
