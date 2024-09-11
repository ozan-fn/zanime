import axios from "axios";
import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { motion } from "framer-motion";

export default function Search() {
    const { query } = useParams();
    const [animes, setAnimes] = useState<{ loading?: boolean; data?: { title: string; url: string; type: string; imageUrl: string }[] }>();

    useEffect(() => {
        (async () => {
            setAnimes((p) => ({ ...p, loading: true }));
            const { data } = await axios.get("/api/kuronime/search?query=" + query);
            setAnimes((p) => ({ ...p, data: data.data }));
            setAnimes((p) => ({ ...p, loading: false }));
        })();
    }, [query]);

    return (
        <>
            <div className="container mx-auto mt-4">
                {animes?.loading && <motion.div animate={{ x: [100, 0, 100] }} transition={{ repeat: Infinity, duration: 2 }} className="h-2 w-12 rounded-lg bg-teal-500" />}
                {animes && (animes?.data?.length ?? 0) > 0 ? (
                    <div className="grid grid-cols-3 gap-4 overflow-hidden md:grid-cols-4 lg:grid-cols-6 xl:grid-cols-8 2xl:grid-cols-10">
                        {animes.data?.map((v, i) => (
                            <Link to={`/anime/${v.url}`} key={i}>
                                <motion.div initial={{ opacity: 0 }} animate={{ opacity: [0, 1], y: [20, 0] }} transition={{ delay: 0.1 * i }} className="relative flex flex-col rounded-lg">
                                    <img src={v.imageUrl} alt={v.title} className="rounded-lg" />
                                    <p className="line-clamp-1 break-words">{v.title}</p>
                                    <p className="font-xs absolute left-2 top-2 rounded-lg bg-teal-500 px-2 py-0 font-medium text-white">{v.type}</p>
                                </motion.div>
                            </Link>
                        ))}
                    </div>
                ) : (
                    <>
                        {/*  */}
                        {!animes?.loading && <p>NO RESULT</p>}
                    </>
                )}
            </div>
        </>
    );
}
