"use client";

import React from "react";
import { api } from "~/trpc/react";

export default function Page() {
    const animes = api.anime.findmany.useQuery();

    return (
        <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6">
            {animes.data?.map((v, i) => {
                return <div key={i}>{v.title}</div>;
            })}
        </div>
    );
}
