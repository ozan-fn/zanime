"use client";

import { ArrowLeftIcon, Loader2Icon } from "lucide-react";
import Image from "next/image";
import { useParams, useRouter } from "next/navigation";
import React, { useEffect } from "react";
import { api } from "~/trpc/react";

export default function Page() {
    const { id } = useParams();
    const anime = api.mal.getAnimeById.useMutation();
    const router = useRouter();

    useEffect(() => {
        anime.mutate(id as string);
    }, [anime, id]);

    return (
        <div className="flex flex-1 flex-col p-4">
            <button type="button" onClick={() => router.back()} className="h-8 w-fit rounded-lg bg-red-500 px-3">
                <ArrowLeftIcon />
            </button>

            {anime.isPending ? (
                <>
                    <Loader2Icon className="mt-4 animate-spin" />
                </>
            ) : (
                <>
                    <div className="mt-4 flex w-full flex-row">
                        <Image src={anime.data?.data.images.jpg.image_url ?? ""} alt={""} height={1080} width={720} className="aspect-[10 / 16] h-fit w-40 shrink-0 rounded-lg object-cover" />
                        <div className="ml-4 flex w-full flex-col">
                            <p className="text-lg font-semibold">{anime.data?.data.title}</p>
                            <p className="text-sm font-semibold text-gray-50/80">{anime.data?.data.title_synonyms}</p>
                            <p className="mt-2 italic">{anime.data?.data.synopsis}</p>
                        </div>
                    </div>

                    <div className="mt-4 flex flex-col">
                        <button className="h-10 w-fit rounded-lg bg-blue-500 px-3">Upload</button>

                        {/* <table className="w-full max-w-6xl table-auto text-left">
                            <thead>
                                <tr className="h-10 border-b border-gray-800">
                                    <th className="px-6">EPS</th>
                                    <th className="w-full px-6">PATH</th>
                                    <th className="w-full px-6"></th>
                                </tr>
                            </thead>
                            <tbody>
                                <tr className="h-10 border-b border-gray-800">
                                    <th className="px-6">1</th>
                                    <td className="px-6">https://asdasdasdasd.com</td>
                                    <td>
                                        <button type="button" className="flex h-8 items-center justify-center rounded-lg bg-gray-800 px-3">
                                            UPLOAD
                                        </button>
                                    </td>
                                </tr>
                            </tbody>
                        </table> */}
                    </div>
                </>
            )}
        </div>
    );
}
