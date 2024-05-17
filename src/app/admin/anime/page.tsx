"use client";

import { useState } from "react";
import { api } from "~/trpc/react";
import { Loader2Icon, X } from "lucide-react";
import Image from "next/image";
import { type Daum } from "~/types/mal";
import { AnimatePresence, motion } from "framer-motion";
import { useSession } from "next-auth/react";

export default function Page() {
    const session = useSession();
    const [input, setInput] = useState("");
    const animes = api.mal.getAnimeSearch.useMutation();
    const [selected, setSelected] = useState<Daum>();
    const [upload, setUpload] = useState(false);
    const upsertAnime = api.anime.upsert.useMutation();
    const [episode, setEpisode] = useState(1);
    const [url, setUrl] = useState("");
    const findUniqueAnime = api.anime.findUnique.useMutation();
    const animess = api.anime.findmany.useQuery();

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
                                    setSelected(v);
                                    findUniqueAnime.mutate({ where: { mal_id: v.mal_id }, include: { Episode: true } });
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
                    {animess.isPending && <Loader2Icon className="animate-spin" />}
                    {animess.error && <pre>{animess.error?.message}</pre>}
                    {animess.data?.map((v, i) => {
                        const val = v as unknown as Daum;
                        return (
                            <div
                                onClick={() => {
                                    setSelected(val);
                                    findUniqueAnime.mutate({ where: { mal_id: v.mal_id }, include: { Episode: true } });
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

            <AnimatePresence mode="wait">
                {selected && (
                    <motion.div animate={{ opacity: [0, 1] }} exit={{ opacity: [1, 0] }} onClick={() => setSelected(undefined)} className="absolute left-0 top-0 z-10 flex h-screen w-screen flex-col overflow-hidden bg-gray-950/80">
                        <div className="flex h-12 w-12 cursor-pointer items-center justify-center self-end">
                            <X />
                        </div>
                        <motion.div animate={{ y: ["100vh", "0vh"] }} exit={{ y: ["0vh", "100vh"] }} transition={{ type: "just" }} onClick={(e) => e.stopPropagation()} className="flex flex-1 overflow-auto rounded-t-2xl border-t border-blue-500 bg-gray-950">
                            <div className="container mx-auto flex flex-1 flex-col p-4">
                                <div className="flex flex-row">
                                    <Image src={selected.images.jpg.image_url} alt="" height={1080} width={720} className="aspect-[10/16] h-fit w-40 rounded-lg object-cover" />
                                    <div className="ml-4 flex max-h-min w-full flex-col">
                                        <p className="text-lg font-medium">{selected.title}</p>
                                        <p className="mt-1 flex-1 overflow-auto text-gray-50/80">{selected.synopsis}</p>
                                        <div className="mt-auto flex flex-row gap-2 pt-2">
                                            {selected.genres.map((v, i) => {
                                                return (
                                                    <div key={i} className="flex h-8 items-center justify-center rounded-lg border border-blue-500 px-3">
                                                        {v.name}
                                                    </div>
                                                );
                                            })}
                                        </div>
                                    </div>
                                </div>

                                <hr className="my-4 border-blue-500" />

                                <div className="">
                                    <button onClick={() => setUpload(!upload)} className="h-10 rounded-lg bg-blue-500 px-3">
                                        Upload
                                    </button>
                                </div>

                                <div className="mt-4 flex flex-col">
                                    {findUniqueAnime.data?.Episode.map((v, i) => {
                                        return (
                                            <div key={i} className="flex flex-row rounded-lg border border-blue-500 p-4">
                                                <iframe src={v.url} allowFullScreen className="aspect-[16/10] h-40 overflow-hidden rounded-lg border border-blue-500"></iframe>
                                                <p className="ml-4 text-lg font-medium">
                                                    {selected.title} - Eps. {v.episode}
                                                </p>
                                            </div>
                                        );
                                    })}
                                </div>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>

            <AnimatePresence>
                <form
                    onSubmit={async (e) => {
                        e.preventDefault();

                        if (!selected || !session.data) return;

                        const { mal_id, ...updateData } = selected;

                        await upsertAnime.mutateAsync({
                            where: {
                                mal_id: mal_id,
                            },
                            create: {
                                userId: session.data.user.id,
                                ...selected,
                                Episode: {
                                    create: {
                                        episode: episode,
                                        url: url,
                                    },
                                },
                            },
                            update: {
                                userId: session.data?.user.id,
                                ...updateData,
                                Episode: {
                                    upsert: {
                                        where: {
                                            episode: episode,
                                        },
                                        create: {
                                            episode: episode,
                                            url: url,
                                        },
                                        update: {
                                            url: url,
                                        },
                                    },
                                },
                            },
                            include: {
                                User: true,
                                Episode: true,
                            },
                        });
                    }}
                >
                    {upload && (
                        <motion.div animate={{ opacity: [0, 1] }} exit={{ opacity: [1, 0] }} onClick={() => setUpload(false)} className="absolute left-0 top-0 z-20 flex h-screen w-screen items-center justify-center bg-gray-950/80">
                            <div onClick={(e) => e.stopPropagation()} className="relative flex w-full max-w-sm flex-col rounded-lg border border-blue-500 bg-gray-950 p-4 md:max-w-md">
                                <p className="text-lg font-medium">Upload</p>
                                <hr className="my-4 border-blue-500" />

                                <p>Episode</p>
                                <input type="number" required value={episode} onChange={(e) => setEpisode(+e.target.value)} className="h-10 rounded-lg border border-blue-500 bg-gray-950 px-3 outline-none" />

                                <p className="mt-2">Embed Url</p>
                                <input onChange={(e) => setUrl(e.target.value)} type="url" required className="h-10 rounded-lg border border-blue-500 bg-gray-950 px-3 outline-none" />

                                <button type="submit" disabled={upsertAnime.isPending} className="mt-4 h-10 rounded-lg bg-blue-500 px-3">
                                    {!upsertAnime.isPending ? <p>Upload</p> : <Loader2Icon className="mx-auto animate-spin" />}
                                </button>

                                <button onClick={() => setUpload(false)} className="absolute right-2 top-2">
                                    <X className="" />
                                </button>
                            </div>
                        </motion.div>
                    )}
                </form>
            </AnimatePresence>
        </>
    );
}
