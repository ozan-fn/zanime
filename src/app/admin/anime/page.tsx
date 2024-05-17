"use client";

import { useEffect, useState } from "react";
import { api } from "~/trpc/react";
import { Loader2Icon } from "lucide-react";
import Image from "next/image";
import { type Daum } from "~/types/mal";
import UploadAnime from "~/components/UploadAnime";

export default function Page() {
    const [input, setInput] = useState("");
    const [anime, setAnime] = useState<Daum>();
    const animes = api.mal.getAnimeSearch.useMutation();
    const findManyAnimes = api.anime.findMany.useQuery();

    const test = api.post.create.useMutation();

    useEffect(() => {
        async function main() {
            test.mutate({ driveId: "0APVAfn6lc68JUk9PVA", anime: anime!, episode: 1, url: "" });
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    async function search() {
        try {
            await animes.mutateAsync(input);
        } catch (error) {
            if (error instanceof Error) {
                console.log(error.message);
            }
        }
    }

    return (
        <>
            <div className="flex-1 p-4">
                <div className="flex flex-row items-center gap-3">
                    <input type="text" name="search" id="search" placeholder="Search" onChange={(e) => setInput(e.target.value)} className="h-10 w-full max-w-sm rounded-lg border border-blue-500 bg-gray-950 px-3 outline-none" />
                    <button type="button" onClick={search} className="flex h-10 items-center justify-center rounded-lg bg-blue-500 px-3">
                        FIND
                    </button>
                </div>

                {animes.data && <p className="mt-4 text-lg font-medium">Results</p>}

                <div className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
                    {animes.isPending && <Loader2Icon className="animate-spin" />}
                    {animes.error && <pre>{animes.error?.message}</pre>}
                    {animes.data?.data.map((v, i) => {
                        return (
                            <div
                                onClick={() => {
                                    setAnime(v);
                                }}
                                key={i}
                                className="relative flex"
                            >
                                <Image src={v.images.jpg.image_url} alt="" height={1080} width={720} className="aspect-[10/16] rounded-lg" />
                            </div>
                        );
                    })}
                </div>

                <p className="mt-4 text-lg font-medium">Uploaded Anime</p>

                <div className="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
                    {findManyAnimes.isPending && <Loader2Icon className="animate-spin" />}
                    {findManyAnimes.error && <pre>{findManyAnimes.error?.message}</pre>}
                    {findManyAnimes.data?.map((v, i) => {
                        const val = v as unknown as Daum;
                        return (
                            <div
                                onClick={() => {
                                    setAnime(val);
                                }}
                                key={i}
                                className="relative flex"
                            >
                                <Image src={v.images?.jpg?.image_url ?? ""} alt="" height={1080} width={720} className="aspect-[10/16] rounded-lg" />
                            </div>
                        );
                    })}
                </div>
            </div>

            {anime && <UploadAnime anime={anime} onClose={() => setAnime(undefined)} />}
        </>
    );
}
