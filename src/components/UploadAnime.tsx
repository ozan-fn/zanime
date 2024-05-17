"use client";

import React, { useEffect, useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { type Daum } from "~/types/mal";
import Image from "next/image";
import { api } from "~/trpc/react";
import { useSession } from "next-auth/react";
import { Loader2Icon, XIcon } from "lucide-react";

export default function UploadAnime({ anime, onClose }: { anime: Daum; onClose: () => void }) {
    const session = useSession();
    const [upload, setUpload] = useState(false);
    const [episode, setEpisode] = useState(1);
    const [url, setUrl] = useState("");
    const findUniqueAnime = api.anime.findUnique.useMutation();
    const upsertAnime = api.anime.upsert.useMutation();

    useEffect(() => {
        findUniqueAnime.mutate({ where: { mal_id: anime.mal_id }, include: { Episode: true } });
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    return (
        <>
            <AnimatePresence mode="wait">
                {anime && (
                    <motion.div animate={{ opacity: [0, 1] }} exit={{ opacity: [1, 0] }} onClick={() => onClose()} className="absolute left-0 top-0 z-10 flex h-screen w-screen flex-col overflow-hidden bg-gray-950/80">
                        <div className="flex h-12 w-12 cursor-pointer items-center justify-center self-end">
                            <XIcon />
                        </div>
                        <motion.div animate={{ y: ["100vh", "0vh"] }} exit={{ y: ["0vh", "100vh"] }} transition={{ type: "just" }} onClick={(e) => e.stopPropagation()} className="flex flex-1 overflow-auto rounded-t-2xl border-t border-blue-500 bg-gray-950">
                            <div className="container mx-auto flex flex-1 flex-col p-4">
                                <div className="flex flex-row">
                                    <Image src={anime.images.jpg.image_url} alt="" height={1080} width={720} className="aspect-[10/16] h-fit w-40 rounded-lg object-cover" />
                                    <div className="ml-4 flex max-h-min w-full flex-col">
                                        <p className="text-lg font-medium">{anime.title}</p>
                                        <p className="mt-1 flex-1 overflow-auto text-gray-50/80">{anime.synopsis}</p>
                                        <div className="mt-auto flex flex-row gap-2 pt-2">
                                            {anime.genres.map((v, i) => {
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

                                <div className="flex flex-col gap-2 py-4">
                                    {Array.from({ length: anime.episodes }).map((_, i) => {
                                        return (
                                            <div key={i} className="flex flex-row items-center rounded-lg border border-blue-500 p-4">
                                                <p className="ml-4 text-lg font-medium">Episode {i + 1}</p>
                                                <p className="ml-4">{findUniqueAnime.data?.Episode.find((f) => f.episode == i + 1)?.url}</p>
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

                        if (!anime || !session.data) return;

                        const { mal_id, ...updateData } = anime;

                        await upsertAnime.mutateAsync({
                            where: {
                                mal_id: mal_id,
                            },
                            create: {
                                userId: session.data.user.id,
                                ...anime,
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
                                    <XIcon className="" />
                                </button>
                            </div>
                        </motion.div>
                    )}
                </form>
            </AnimatePresence>
        </>
    );
}
