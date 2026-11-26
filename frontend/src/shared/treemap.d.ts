export interface TmNode {
  i: number;
  n: string;
  s: number;
  d?: boolean;
  e?: string;
  o?: number;
  c?: TmNode[];
}

export interface TreemapOptions {
  onClick?: (node: TmNode, e: MouseEvent, path: TmNode[]) => void;
  onContext?: (node: TmNode | null, e: MouseEvent, path: TmNode[] | null) => void;
  format?: (bytes: number) => string;
  othersLabel?: (count: number) => string;
  dark?: boolean;
}

export declare class Treemap {
  constructor(container: HTMLElement, opts?: TreemapOptions);
  setData(root: TmNode): void;
  select(id: number | null): void;
  render(): void;
  destroy(): void;
}

declare global {
  interface Window {
    CadastreTreemap: {
      Treemap: typeof Treemap;
      colors: Record<string, number[]>;
      families: string[];
      familyOf(n: TmNode): string;
      find(root: TmNode, id: number): TmNode | null;
    };
  }
}
