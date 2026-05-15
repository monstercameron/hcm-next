import { brandTokensToCssVariables } from "@hcm-next/ui-runtime";
import type { BrandPack } from "@hcm-next/ui-contracts";
import type { CSSProperties, ReactNode } from "react";

type BrandTokenProviderProps = {
  brand: BrandPack;
  children: ReactNode;
  styleOverrides?: CSSProperties;
};

export function BrandTokenProvider({
  brand,
  children,
  styleOverrides,
}: BrandTokenProviderProps): JSX.Element {
  const style = {
    ...brandTokensToCssVariables(brand.tokens),
    "--font-body": brand.typography.bodyFontFamily,
    "--font-heading": brand.typography.headingFontFamily,
    "--font-mono": brand.typography.monoFontFamily,
    ...styleOverrides,
  } as CSSProperties;

  return (
    <div
      className={`brand-root brand-density-${brand.density} brand-radius-${brand.radius}`}
      style={style}
    >
      {children}
    </div>
  );
}
