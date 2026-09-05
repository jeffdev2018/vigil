import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";

export interface CloudRuntimeNode {
  id: string;
  owner_id: string;
  instance_id: string;
  region: string;
  instance_type: string;
  image_id: string;
  subnet_id: string;
  name: string;
  status: string;
  tags: Record<string, string>;
  metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

/**
 * Power actions the fleet exposes for one node. `status` is a read, the other
 * three are writes; all four are POSTs carrying `{ instance_id }`.
 */
export type CloudRuntimeNodeAction = "start" | "stop" | "reboot";

/**
 * Shape of an action / status response. The fleet service lives outside this
 * repo, so only the two fields the UI consumes are modelled; the schema keeps
 * everything else (see CloudRuntimeNodeActionSchema).
 */
export interface CloudRuntimeNodeActionResult {
  instance_id: string;
  status: string;
}

export interface ListCloudRuntimeNodesParams {
  limit?: number;
  offset?: number;
}

export interface CreateCloudRuntimeNodeRequest {
  instance_type: string;
  name?: string;
  region?: string;
  image_id?: string;
  subnet_id?: string;
  key_name?: string;
  iam_instance_profile?: string;
  disk_size_gb?: number;
  tags?: Record<string, string>;
}

export const cloudRuntimeKeys = {
  all: (wsId: string) => ["cloud-runtime", wsId] as const,
  nodes: (wsId: string) => [...cloudRuntimeKeys.all(wsId), "nodes"] as const,
};

const PENDING_NODE_STATUSES = new Set([
  "launching",
  "pending",
  "starting",
  "stopping",
  "rebooting",
  "terminating",
]);

export function isCloudRuntimeNodePending(status: string): boolean {
  return PENDING_NODE_STATUSES.has(status.toLowerCase());
}

export function cloudRuntimeNodeListOptions(
  wsId: string,
  params?: ListCloudRuntimeNodesParams,
) {
  const limit = params?.limit ?? 20;
  const offset = params?.offset ?? 0;
  return queryOptions({
    queryKey: [...cloudRuntimeKeys.nodes(wsId), { limit, offset }] as const,
    queryFn: () => api.listCloudRuntimeNodes({ limit, offset }),
    refetchInterval: (query) =>
      query.state.data?.some((node) => isCloudRuntimeNodePending(node.status))
        ? 5000
        : false,
    staleTime: 15 * 1000,
  });
}

export function useCreateCloudRuntimeNode(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateCloudRuntimeNodeRequest) =>
      api.createCloudRuntimeNode(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: cloudRuntimeKeys.all(wsId) });
    },
  });
}

export function useDeleteCloudRuntimeNode(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (instanceId: string) => api.deleteCloudRuntimeNode(instanceId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: cloudRuntimeKeys.all(wsId) });
    },
  });
}

export function useCloudRuntimeNodeAction(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      action,
      instanceId,
    }: {
      action: CloudRuntimeNodeAction;
      instanceId: string;
    }) => api.actOnCloudRuntimeNode(action, instanceId),
    // A power action starts a transition rather than finishing one, so the
    // list is refetched (and keeps polling while any node reads as pending)
    // instead of the row being patched to a guessed end state.
    onSettled: () => {
      qc.invalidateQueries({ queryKey: cloudRuntimeKeys.all(wsId) });
    },
  });
}

/**
 * On-demand refresh of one node's status. The result is patched into the
 * cached list: the value is determinate (it IS the server's answer), the user
 * stays on the same screen, and a failure changes nothing.
 */
export function useCloudRuntimeNodeStatus(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (instanceId: string) => api.getCloudRuntimeNodeStatus(instanceId),
    onSuccess: (result, instanceId) => {
      if (!result.status) return;
      qc.setQueriesData<CloudRuntimeNode[]>(
        { queryKey: cloudRuntimeKeys.nodes(wsId) },
        (nodes) =>
          nodes?.map((node) =>
            node.instance_id === instanceId
              ? { ...node, status: result.status }
              : node,
          ),
      );
    },
  });
}
