"use client";

import { SessionProvider } from "next-auth/react";
import { type ReactNode } from "react";
import { TRPCReactProvider } from "~/trpc/react";

export default function Providers(props: { children: ReactNode }) {
    return (
        <TRPCReactProvider>
            <SessionProvider>
                <>{props.children}</>
            </SessionProvider>
        </TRPCReactProvider>
    );
}
