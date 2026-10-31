"use client";
import { Icon } from "@/lib/icons";
import { CATALOG, type CatalogItem } from "@/lib/catalog";

export function Palette(props: { hidden: boolean; onAdd: (c: CatalogItem) => void }) {
  return (
    <aside className={`palette${props.hidden ? " hidden" : ""}`}>
      <h3>Components</h3>
      <p className="sub">Click to add a component, then drag from its right port to another node to wire it.</p>
      <div className="pal-list">
        {CATALOG.map((c) => (
          <button key={c.type} className="pal-item" onClick={() => props.onAdd(c)}>
            <span className={`n-ico kind-${c.kind}`} style={{ width: 24, height: 24 }}>
              <Icon name={c.icon} size={14} />
            </span>
            <span>
              {c.name}
              <small>{c.desc}</small>
            </span>
          </button>
        ))}
      </div>
    </aside>
  );
}
