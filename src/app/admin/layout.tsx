"use client";

import { BoxIcon, HomeIcon } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import React, { type ReactNode } from "react";

export default function Layout(props: { children?: ReactNode }) {
    const pathname = usePathname();
    const sidebar = [
        {
            name: "Dashboard",
            href: "/admin",
            icon: HomeIcon,
        },
        {
            name: "Anime",
            href: "/admin/anime",
            icon: BoxIcon,
        },
    ];

    return (
        <div className="flex min-h-screen bg-gray-950 text-gray-50">
            <div className="absolute left-0 top-0 flex h-screen w-screen overflow-auto pl-64 pt-16">
                <div className="flex-1 overflow-auto">
                    <>{props.children}</>
                </div>
            </div>
            <div className="absolute left-0 top-0 h-16 w-screen border-b border-blue-500 pl-64">
                <></>
            </div>
            <div className="absolute left-0 top-0 h-screen w-64 border-r border-blue-500">
                <div className="flex h-16 w-full items-center justify-center">
                    <p className="text-lg font-semibold">ZANIME</p>
                </div>
                <div className="flex flex-col gap-1 p-2">
                    {sidebar.map((v, i) => {
                        const isActive = `/${pathname.split("/").slice(1, 3).join("/")}` == v.href;
                        return (
                            <Link href={v.href} key={i} className={`flex h-12 w-full flex-row items-center gap-3 rounded-lg border border-blue-500 px-3 ${isActive && "bg-blue-500"}`}>
                                <v.icon />
                                <p>{v.name}</p>
                            </Link>
                        );
                    })}
                </div>
            </div>
        </div>
    );
}
