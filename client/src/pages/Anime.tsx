import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { motion } from "framer-motion";
import axios from "axios";
import { useProvider } from "../layouts/MainLayout";
import { AnimeDetail, Embed } from "../types/animes";

export default function Anime() {
    const { title } = useParams();
    const [anime, setAnime] = useState<AnimeDetail>();
    const [episode, setEpisode] = useState("");
    const [server, setServer] = useState({ loading: false, value: "" });
    const [servers, setServers] = useState<{ loading?: boolean; value?: Embed }>();
    const { provider } = useProvider();

    useEffect(() => {
        (async () => {
            if (!provider) return;
            const { data } = await axios.get(`/api/${provider}/episode/${title}`);
            setAnime(data);
        })();
    }, [provider]);

    useEffect(() => {
        (async () => {
            if (!provider) return;
            if (!episode) return;
            setServers({ loading: true });
            const { data } = await axios.get(`/api/${provider}/episode/embed/${episode}`);
            setServers((p) => ({ ...p, value: data, loading: false }));
        })();
    }, [episode, provider]);

    return (
        <>
            <div className="container mx-auto mt-4 flex flex-row items-center rounded-lg border border-white/10">
                <div className="mr-4 h-10 w-1 rounded-lg bg-teal-500" />
                <p className="text-lg font-medium">
                    <span className="line-clamp-1">
                        {anime?.title} ({anime?.status})
                    </span>
                </p>
            </div>

            <div className="container relative mx-auto mt-4 flex justify-center">
                <iframe src={server.value.includes("https://") ? server.value : ""} allowFullScreen frameBorder={0} className="aspect-video w-full max-w-md border border-zinc-700 bg-white/10 md:max-w-2xl xl:max-w-3xl 2xl:max-w-4xl"></iframe>
                {server.loading && <motion.div animate={{ x: [-50, 50, -50] }} transition={{ repeat: Infinity, duration: 2 }} className="absolute left-1/2 top-1/2 h-2 w-12 -translate-x-1/2 -translate-y-1/2 rounded-lg bg-teal-500" />}
            </div>

            <div className="container mx-auto mt-4 flex flex-row items-center rounded-lg border border-white/10">
                <div className="mr-4 h-10 w-1 rounded-lg bg-teal-500" />
                <p className="text-lg font-medium">
                    <span className="line-clamp-1">PILIH SERVER</span>
                </p>
            </div>

            <div className="container mx-auto mt-4 grid grid-flow-row-dense grid-cols-4 gap-2 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7 2xl:grid-cols-8">
                {servers?.value &&
                    Object.entries(servers.value)?.map((v, i) => {
                        const isSelected = v[0] == server.value;
                        const res = Object.entries(v[1]);
                        return (
                            <>
                                <div key={i} className="flex flex-col gap-2">
                                    <button onClick={() => setServer({ loading: true, value: v[0] })} className={`flex h-10 items-center justify-center rounded-lg ${isSelected ? "bg-teal-500 text-white" : "border border-zinc-700"}`} key={i}>
                                        <p className="">{v[0]}</p>
                                    </button>
                                    {isSelected && (
                                        <>
                                            {res.map((v, i) => {
                                                if (!v[1]) return;
                                                return (
                                                    <button
                                                        key={i}
                                                        onClick={() => {
                                                            setServer({ loading: false, value: v[1] ?? "" });
                                                        }}
                                                        className="h-10 w-full rounded-lg border border-white/10"
                                                    >
                                                        {v[0]}
                                                    </button>
                                                );
                                            })}
                                        </>
                                    )}
                                </div>
                            </>
                        );
                    })}
            </div>

            <div className="container mx-auto mt-4 flex flex-row items-center rounded-lg border border-white/10">
                <div className="mr-4 h-10 w-1 rounded-lg bg-teal-500" />
                <p className="text-lg font-medium">
                    <span className="line-clamp-1">PILIH EPISODE</span>
                </p>
            </div>

            <div className="container mx-auto mb-8 mt-4 grid grid-flow-row-dense grid-cols-3 gap-2 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
                {anime?.episodeList?.map((v, i) => {
                    const selected = v.url == episode;
                    return (
                        <>
                            {selected ? (
                                <button disabled={servers?.loading} onClick={() => setEpisode(v.url)} className="flex h-10 items-center justify-center rounded-lg bg-teal-500 text-white" key={i}>
                                    <p className="line-clamp-1">EPISODE {(i - anime.episodeList.length) * -1}</p>
                                </button>
                            ) : (
                                <button disabled={servers?.loading} onClick={() => setEpisode(v.url)} className="flex h-10 items-center justify-center rounded-lg border border-white/10" key={i}>
                                    <p className="line-clamp-1">EPISODE {(i - anime.episodeList.length) * -1}</p>
                                </button>
                            )}
                        </>
                    );
                })}
            </div>
        </>
    );
}
