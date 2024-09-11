import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { motion } from "framer-motion";
import axios from "axios";

export default function Anime() {
    const { title } = useParams();
    const [loading, setLoading] = useState(false);
    const [episode, setEpisode] = useState("");
    const [episodes, setEpisodes] = useState<{ title: string; url: string }[]>();
    const [server, setServer] = useState("");
    const [servers, setServers] = useState<{ [key: string]: { [key: string]: string | null } }>();
    const [listServer, setListServer] = useState("");

    useEffect(() => {
        (async () => {
            const { data } = await axios.get(`/api/kuronime/episode/${title}`);
            setEpisodes(data.data);
        })();
    }, []);

    useEffect(() => {
        (async () => {
            setLoading(true);
            setServers(undefined);
            if (!episode) return;
            const { data } = await axios.get(`/api/kuronime/episode/embed/${episode}`);
            setServers(data.data.embed);
            setLoading(false);
        })();
    }, [episode]);

    return (
        <>
            <div className="container mx-auto mt-4 flex flex-row items-center rounded-lg border border-white/10">
                <div className="mr-4 h-10 w-1 rounded-lg bg-teal-500" />
                <p className="text-lg font-medium">
                    <span className="line-clamp-1">{title}</span>
                </p>
            </div>

            <div className="container relative mx-auto mt-4 flex justify-center">
                <iframe src={server} allowFullScreen frameBorder={0} className="aspect-video w-full max-w-md border border-zinc-700 bg-white/10 md:max-w-2xl xl:max-w-3xl 2xl:max-w-4xl"></iframe>
                {loading && <motion.div animate={{ x: [-50, 50, -50] }} transition={{ repeat: Infinity, duration: 2 }} className="absolute left-1/2 top-1/2 h-2 w-12 -translate-x-1/2 -translate-y-1/2 rounded-lg bg-teal-500" />}
            </div>

            <div className="container mx-auto mt-4 flex flex-row items-center rounded-lg border border-white/10">
                <div className="mr-4 h-10 w-1 rounded-lg bg-teal-500" />
                <p className="text-lg font-medium">
                    <span className="line-clamp-1">PILIH SERVER</span>
                </p>
            </div>

            <div className="container mx-auto mt-4 grid grid-flow-row-dense grid-cols-4 gap-2 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7 2xl:grid-cols-8">
                {servers &&
                    Object.entries(servers)?.map((v, i) => {
                        const isSelected = v[0] == listServer;
                        const res = Object.entries(v[1]);
                        return (
                            <>
                                <div key={i} className="flex flex-col gap-2">
                                    <button onClick={() => setListServer(v[0])} className={`flex h-10 items-center justify-center rounded-lg ${isSelected ? "bg-teal-500 text-white" : "border border-zinc-700"}`} key={i}>
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
                                                            setLoading(true);
                                                            setServer(v[1] ?? "");
                                                            setLoading(false);
                                                            setListServer("");
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
                {episodes?.map((v, i) => {
                    const selected = v.url == episode;
                    return (
                        <>
                            {selected ? (
                                <button className="flex h-10 items-center justify-center rounded-lg bg-teal-500 text-white" key={i}>
                                    <p className="line-clamp-1">{v.title}</p>
                                </button>
                            ) : (
                                <button onClick={() => setEpisode(v.url)} className="flex h-10 items-center justify-center rounded-lg border border-white/10" key={i}>
                                    <p className="line-clamp-1">{v.title}</p>
                                </button>
                            )}
                        </>
                    );
                })}
            </div>
        </>
    );
}
