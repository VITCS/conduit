"use client";
import { createContext, useContext } from "react";

export interface EditorCtx {
  editMode: boolean;
  setEdgeLabel: (edgeId: string, label: string) => void;
  deleteEdge: (edgeId: string) => void;
  // Project-level config for resolving ${var:…}/${secret:…} into display values on
  // nodes, so each node shows what it actually reads.
  vars: Record<string, string>;
  secretNames: string[];
}

export const EditorContext = createContext<EditorCtx>({
  editMode: false,
  setEdgeLabel: () => {},
  deleteEdge: () => {},
  vars: {},
  secretNames: [],
});

export const useEditor = () => useContext(EditorContext);
