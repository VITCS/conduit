"use client";
import { createContext, useContext } from "react";

export interface EditorCtx {
  editMode: boolean;
  setEdgeLabel: (edgeId: string, label: string) => void;
  deleteEdge: (edgeId: string) => void;
}

export const EditorContext = createContext<EditorCtx>({
  editMode: false,
  setEdgeLabel: () => {},
  deleteEdge: () => {},
});

export const useEditor = () => useContext(EditorContext);
